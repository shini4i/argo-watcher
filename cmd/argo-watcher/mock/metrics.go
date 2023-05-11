// Code generated by MockGen. DO NOT EDIT.
// Source: cmd/argo-watcher/metrics.go

// Package mock is a generated GoMock package.
package mock

import (
	reflect "reflect"

	gomock "github.com/golang/mock/gomock"
)

// MockMetricsInterface is a mock of MetricsInterface interface.
type MockMetricsInterface struct {
	ctrl     *gomock.Controller
	recorder *MockMetricsInterfaceMockRecorder
}

// MockMetricsInterfaceMockRecorder is the mock recorder for MockMetricsInterface.
type MockMetricsInterfaceMockRecorder struct {
	mock *MockMetricsInterface
}

// NewMockMetricsInterface creates a new mock instance.
func NewMockMetricsInterface(ctrl *gomock.Controller) *MockMetricsInterface {
	mock := &MockMetricsInterface{ctrl: ctrl}
	mock.recorder = &MockMetricsInterfaceMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockMetricsInterface) EXPECT() *MockMetricsInterfaceMockRecorder {
	return m.recorder
}

// AddFailedDeployment mocks base method.
func (m *MockMetricsInterface) AddFailedDeployment(app string) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "AddFailedDeployment", app)
}

// AddFailedDeployment indicates an expected call of AddFailedDeployment.
func (mr *MockMetricsInterfaceMockRecorder) AddFailedDeployment(app interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AddFailedDeployment", reflect.TypeOf((*MockMetricsInterface)(nil).AddFailedDeployment), app)
}

// AddProcessedDeployment mocks base method.
func (m *MockMetricsInterface) AddProcessedDeployment() {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "AddProcessedDeployment")
}

// AddProcessedDeployment indicates an expected call of AddProcessedDeployment.
func (mr *MockMetricsInterfaceMockRecorder) AddProcessedDeployment() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "AddProcessedDeployment", reflect.TypeOf((*MockMetricsInterface)(nil).AddProcessedDeployment))
}

// ResetFailedDeployment mocks base method.
func (m *MockMetricsInterface) ResetFailedDeployment(app string) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "ResetFailedDeployment", app)
}

// ResetFailedDeployment indicates an expected call of ResetFailedDeployment.
func (mr *MockMetricsInterfaceMockRecorder) ResetFailedDeployment(app interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ResetFailedDeployment", reflect.TypeOf((*MockMetricsInterface)(nil).ResetFailedDeployment), app)
}

// SetArgoUnavailable mocks base method.
func (m *MockMetricsInterface) SetArgoUnavailable(unavailable bool) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "SetArgoUnavailable", unavailable)
}

// SetArgoUnavailable indicates an expected call of SetArgoUnavailable.
func (mr *MockMetricsInterfaceMockRecorder) SetArgoUnavailable(unavailable interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SetArgoUnavailable", reflect.TypeOf((*MockMetricsInterface)(nil).SetArgoUnavailable), unavailable)
}
