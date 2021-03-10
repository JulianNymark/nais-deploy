// Code generated by mockery v2.6.0. DO NOT EDIT.

package pb

import (
	context "context"

	grpc "google.golang.org/grpc"

	mock "github.com/stretchr/testify/mock"
)

// MockDeployClient is an autogenerated mock type for the DeployClient type
type MockDeployClient struct {
	mock.Mock
}

// Deploy provides a mock function with given fields: ctx, in, opts
func (_m *MockDeployClient) Deploy(ctx context.Context, in *DeploymentRequest, opts ...grpc.CallOption) (*DeploymentStatus, error) {
	_va := make([]interface{}, len(opts))
	for _i := range opts {
		_va[_i] = opts[_i]
	}
	var _ca []interface{}
	_ca = append(_ca, ctx, in)
	_ca = append(_ca, _va...)
	ret := _m.Called(_ca...)

	var r0 *DeploymentStatus
	if rf, ok := ret.Get(0).(func(context.Context, *DeploymentRequest, ...grpc.CallOption) *DeploymentStatus); ok {
		r0 = rf(ctx, in, opts...)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*DeploymentStatus)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(context.Context, *DeploymentRequest, ...grpc.CallOption) error); ok {
		r1 = rf(ctx, in, opts...)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// Status provides a mock function with given fields: ctx, in, opts
func (_m *MockDeployClient) Status(ctx context.Context, in *DeploymentRequest, opts ...grpc.CallOption) (Deploy_StatusClient, error) {
	_va := make([]interface{}, len(opts))
	for _i := range opts {
		_va[_i] = opts[_i]
	}
	var _ca []interface{}
	_ca = append(_ca, ctx, in)
	_ca = append(_ca, _va...)
	ret := _m.Called(_ca...)

	var r0 Deploy_StatusClient
	if rf, ok := ret.Get(0).(func(context.Context, *DeploymentRequest, ...grpc.CallOption) Deploy_StatusClient); ok {
		r0 = rf(ctx, in, opts...)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(Deploy_StatusClient)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func(context.Context, *DeploymentRequest, ...grpc.CallOption) error); ok {
		r1 = rf(ctx, in, opts...)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}
