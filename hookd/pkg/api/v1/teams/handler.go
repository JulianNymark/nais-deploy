package api_v1_teams

import (
	"encoding/json"
	"net/http"

	"github.com/navikt/deployment/hookd/pkg/database"
	"github.com/navikt/deployment/hookd/pkg/middleware"
	log "github.com/sirupsen/logrus"
)

type TeamsHandler struct {
	APIKeyStorage database.Database
}

type team struct {
	Team    string `json:"team"`
	GroupId string `json:"groupId"`
}

func (h *TeamsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	groups := r.Context().Value("groups").([]string)

	fields := middleware.RequestLogFields(r)
	logger := log.WithFields(fields)
	keys := []database.ApiKey{}
	for _, group := range groups {
		apiKeys, err := h.APIKeyStorage.ReadByGroupClaim(group)
		if err != nil {
			logger.Error(err)
		}
		if len(apiKeys) > 0 {
			for _, apiKey := range apiKeys {
				keys = append(keys, apiKey)
			}
		}
	}
	var teams []team
	for _, v := range keys {
		t := team{
			GroupId: v.GroupId,
			Team:    v.Team,
		}
		teams = append(teams, t)
	}
	teamsJson, err := json.Marshal(teams)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Unable to marshall the team keys"))
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write(teamsJson)
}
