package deployclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	ocodes "go.opentelemetry.io/otel/codes"
	"google.golang.org/grpc/codes"

	"github.com/nais/deploy/pkg/hookd/logproxy"
	"github.com/nais/deploy/pkg/pb"
	"github.com/nais/deploy/pkg/telemetry"
)

type TemplateVariables map[string]interface{}

const (
	DefaultRef                   = "master"
	DefaultOwner                 = "navikt"
	DefaultDeployServer          = "deploy.nav.cloud.nais.io:443"
	DefaultOtelCollectorEndpoint = "https://collector-internet.external.prod-gcp.nav.cloud.nais.io"
	DefaultTracingDashboardURL   = "https://grafana.nav.cloud.nais.io/d/cdxgyzr3rikn4a/deploy-tracing-drilldown?var-trace_id="
	DefaultDeployTimeout         = time.Minute * 10
)

var (
	ErrResourceRequired       = errors.New("at least one Kubernetes resource is required to make sense of the deployment")
	ErrAuthRequired           = errors.New("Github token or API key required")
	ErrClusterRequired        = errors.New("cluster required; see reference section in the documentation for available environments")
	ErrMalformedAPIKey        = errors.New("API key must be a hex encoded string")
	ErrInvalidTelemetryFormat = errors.New("telemetry input format malformed")
)

type Deployer struct {
	Client pb.DeployClient
}

func Prepare(ctx context.Context, cfg *Config) (*pb.DeploymentRequest, error) {
	var err error
	templateVariables := make(TemplateVariables)

	if len(cfg.VariablesFile) > 0 {
		templateVariables, err = templateVariablesFromFile(cfg.VariablesFile)
		if err != nil {
			return nil, Errorf(ExitInvocationFailure, "load template variables: %s", err)
		}
	}

	if len(cfg.Variables) > 0 {
		templateOverrides := templateVariablesFromSlice(cfg.Variables)
		for key, val := range templateOverrides {
			if oldval, ok := templateVariables[key]; ok {
				log.Warnf("Overwriting template variable '%s'; previous value was '%v'", key, oldval)
			}
			log.Infof("Setting template variable '%s' to '%v'", key, val)
			templateVariables[key] = val
		}
	}

	resources := make([]json.RawMessage, 0)

	for i, path := range cfg.Resource {
		parsed, err := MultiDocumentFileAsJSON(path, templateVariables)
		if err != nil {
			if cfg.PrintPayload {
				errStr := err.Error()[len(path)+2:]
				line, er := detectErrorLine(errStr)
				if er == nil {
					ctx := errorContext(string(resources[i]), line)
					for _, l := range ctx {
						fmt.Println(l)
					}
				}
			}
			return nil, ErrorWrap(ExitTemplateError, err)
		}
		resources = append(resources, parsed...)
	}

	if len(cfg.Team) == 0 {
		log.Infof("Team not explicitly specified; attempting auto-detection...")
		for i, path := range cfg.Resource {
			team := detectTeam(resources[i])
			if len(team) > 0 {
				log.Infof("Detected team %q in %q", team, path)
				cfg.Team = team
				break
			}

			team = detectNamespace(resources[i])
			if len(team) > 0 {
				log.Infof("Detected team %q from namespace in %q", team, path)
				cfg.Team = team
				break
			}
		}

		if len(cfg.Team) == 0 {
			return nil, Errorf(ExitInvocationFailure, "no team specified, and unable to auto-detect from nais.yaml")
		}
	}

	if len(cfg.Environment) == 0 {
		log.Infof("Environment not explicitly specified; attempting auto-detection...")

		namespaces := make(map[string]interface{})
		cfg.Environment = cfg.Cluster

		for i := range cfg.Resource {
			namespace := detectNamespace(resources[i])
			namespaces[namespace] = new(interface{})
		}

		if len(namespaces) == 1 {
			for namespace := range namespaces {
				if len(namespace) != 0 {
					cfg.Environment = fmt.Sprintf("%s:%s", cfg.Cluster, namespace)
				}
			}
		}

		log.Infof("Detected environment '%s'", cfg.Environment)
	}

	for i := range resources {
		resources[i], err = InjectAnnotations(resources[i], BuildEnvironmentAnnotations())
		if err != nil {
			return nil, ErrorWrap(ExitInternalError, fmt.Errorf("inject annotations in resource %d: %w", i, err))
		}
	}

	allResources, err := wrapResources(resources)
	if err != nil {
		return nil, ErrorWrap(ExitInvocationFailure, err)
	}

	kube, err := pb.KubernetesFromJSONResources(allResources)
	if err != nil {
		return nil, ErrorWrap(ExitInvocationFailure, err)
	}

	deadline, _ := ctx.Deadline()

	return MakeDeploymentRequest(*cfg, deadline, kube), nil
}

func (d *Deployer) Deploy(ctx context.Context, cfg *Config, deployRequest *pb.DeploymentRequest) error {
	var deployStatus *pb.DeploymentStatus
	var err error

	// Root span for tracing.
	// All sub-spans must be created from this context.
	ctx, span := telemetry.Tracer().Start(ctx, "Send deploy request and wait for completion")
	defer span.End()
	deployRequest.TraceParent = telemetry.TraceParentHeader(ctx)

	log.Infof("Sending deployment request to NAIS deploy at %s...", cfg.DeployServerURL)

	sendDeploymentRequest := func() error {
		requestContext, requestSpan := telemetry.Tracer().Start(ctx, "Waiting for deploy server")
		defer requestSpan.End()

		err = retryUnavailable(cfg.RetryInterval, cfg.Retry, func() error {
			deployStatus, err = d.Client.Deploy(requestContext, deployRequest)
			return err
		})

		if err != nil {
			code := grpcErrorCode(err)
			err = fmt.Errorf(formatGrpcError(err))
			if requestContext.Err() != nil {
				requestSpan.SetStatus(ocodes.Error, requestContext.Err().Error())
				return Errorf(ExitTimeout, "deployment timed out: %s", requestContext.Err())
			}
			if code == codes.Unauthenticated {
				if !strings.HasSuffix(cfg.Environment, ":"+cfg.Team) {
					log.Warnf("hint: team %q does not match namespace in %q", cfg.Team, cfg.Environment)
				}
			}
			requestSpan.SetStatus(ocodes.Error, err.Error())
			return ErrorWrap(ExitNoDeployment, err)
		}

		log.Infof("Deployment request accepted by NAIS deploy and dispatched to cluster '%s'.", deployStatus.GetRequest().GetCluster())

		deployRequest.ID = deployStatus.GetRequest().GetID()
		telemetry.AddDeploymentRequestSpanAttributes(span, deployStatus.GetRequest())
		telemetry.AddDeploymentRequestSpanAttributes(requestSpan, deployStatus.GetRequest())

		return nil
	}

	err = sendDeploymentRequest()

	// First handle errors that might have occurred with the request itself.
	// Errors from underlying systems are handled later.
	if err != nil {
		span.SetStatus(ocodes.Error, err.Error())
		span.RecordError(err)
		return err
	}

	traceID := telemetry.TraceID(ctx)

	// Print information to standard output
	urlPrefix := "https://" + strings.Split(cfg.DeployServerURL, ":")[0]
	log.Infof("Deployment information:")
	log.Infof("---")
	log.Infof("id...........: %s", deployRequest.GetID())
	log.Infof("tracing......: %s", cfg.TracingDashboardURL+traceID)
	log.Infof("debug logs...: %s", logproxy.MakeURL(urlPrefix, deployRequest.GetID(), deployRequest.GetTime().AsTime(), deployRequest.Cluster))
	log.Infof("deadline.....: %s", deployRequest.GetDeadline().AsTime().Local())
	log.Infof("---")

	// If running in GitHub actions, print a markdown summary
	summaryFile, err := os.OpenFile(os.Getenv("GITHUB_STEP_SUMMARY"), os.O_APPEND|os.O_WRONLY, 0644)
	summary := func(format string, a ...any) {
		if summaryFile == nil {
			return
		}
		_, _ = fmt.Fprintf(summaryFile, format+"\n", a...)
	}
	finalStatus := func(st *pb.DeploymentStatus) {
		summary("* Finished at: %s", st.Timestamp())
		summary("")
		summary("%c Final status: *%s* / %s", deployStatus.GetState().StatusEmoji(), deployStatus.GetState(), deployStatus.GetMessage())
	}
	if err == nil {
		defer summaryFile.Close()
	}

	summary("# 🚀 NAIS deploy")
	summary("* Detailed trace: [%s](%s)", traceID, cfg.TracingDashboardURL+traceID)
	summary("* Request ID: %s", deployRequest.GetID())
	summary("* Started at: %s", time.Now().Local())
	summary("* Deadline: %s", deployRequest.GetDeadline().AsTime().Local())

	if deployStatus.GetState().Finished() {
		finalStatus(deployStatus)
		logDeployStatus(deployStatus)
		return ErrorStatus(deployStatus)
	}

	if !cfg.Wait {
		finalStatus(deployStatus)
		logDeployStatus(deployStatus)
		return nil
	}

	var stream pb.Deploy_StatusClient
	var connectionLost bool

	log.Infof("Waiting for deployment to complete...")

	for ctx.Err() == nil {
		err = retryUnavailable(cfg.RetryInterval, cfg.Retry, func() error {
			stream, err = d.Client.Status(ctx, deployRequest)
			if err != nil {
				connectionLost = true
			} else if connectionLost {
				log.Infof("Connection to NAIS deploy re-established.")
			}
			return err
		})
		if err != nil {
			summary("❌ lost connection to NAIS deploy", deployStatus.GetState(), deployStatus.GetMessage())
			return ErrorWrap(ExitUnavailable, err)
		}

		for ctx.Err() == nil {
			deployStatus, err = stream.Recv()
			if err != nil {
				connectionLost = true
				if cfg.Retry && grpcErrorRetriable(err) {
					log.Warnf(formatGrpcError(err))
					break
				} else {
					summary("❌ lost connection to NAIS deploy", deployStatus.GetState(), deployStatus.GetMessage())
					return Errorf(ExitUnavailable, formatGrpcError(err))
				}
			}
			logDeployStatus(deployStatus)
			if deployStatus.GetState() == pb.DeploymentState_inactive {
				log.Warnf("NAIS deploy has been restarted. Re-sending deployment request...")
				err = sendDeploymentRequest()
				if err != nil {
					summary("❌ lost connection to NAIS deploy", deployStatus.GetState(), deployStatus.GetMessage())
					return err
				}
			} else if deployStatus.GetState().Finished() {
				finalStatus(deployStatus)
				return ErrorStatus(deployStatus)
			}
		}
	}

	summary("❌ timeout", deployStatus.GetState(), deployStatus.GetMessage())
	return Errorf(ExitTimeout, "deployment timed out: %w", ctx.Err())
}

func grpcErrorRetriable(err error) bool {
	switch grpcErrorCode(err) {
	case codes.Unavailable, codes.Internal:
		return true
	default:
		return false
	}
}

func retryUnavailable(interval time.Duration, retry bool, fn func() error) error {
	for {
		err := fn()
		if retry && grpcErrorRetriable(err) {
			log.Warnf("%s (retrying in %s...)", formatGrpcError(err), interval)
			time.Sleep(interval)
			continue
		}
		return err
	}
}
