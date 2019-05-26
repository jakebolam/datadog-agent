// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-2019 Datadog, Inc.
// +build !windows

package systemd

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/DataDog/datadog-agent/pkg/aggregator/mocksender"
	"github.com/DataDog/datadog-agent/pkg/metrics"
	"github.com/coreos/go-systemd/dbus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type mockSystemdStats struct {
	mock.Mock
}

func (s *mockSystemdStats) NewConn() (*dbus.Conn, error) {
	return nil, nil
}

func (s *mockSystemdStats) CloseConn(c *dbus.Conn) {
}

func (s *mockSystemdStats) ListUnits(conn *dbus.Conn) ([]dbus.UnitStatus, error) {
	args := s.Mock.Called(conn)
	return args.Get(0).([]dbus.UnitStatus), args.Error(1)
}

func (s *mockSystemdStats) TimeNanoNow() int64 {
	args := s.Mock.Called()
	return args.Get(0).(int64)
}

func (s *mockSystemdStats) GetUnitTypeProperties(conn *dbus.Conn, unitName string, unitType string) (map[string]interface{}, error) {
	args := s.Mock.Called(conn, unitName, unitType)
	return args.Get(0).(map[string]interface{}), args.Error(1)
}

func TestDefaultConfiguration(t *testing.T) {
	check := Check{}
	check.Configure([]byte(``), []byte(``))

	assert.Equal(t, []string(nil), check.config.instance.UnitNames)
	assert.Equal(t, []string(nil), check.config.instance.UnitRegexStrings)
	assert.Equal(t, []*regexp.Regexp(nil), check.config.instance.UnitRegexPatterns)
}

func TestConfiguration(t *testing.T) {
	// setup data
	check := Check{}
	rawInstanceConfig := []byte(`
unit_names:
 - ssh.service
 - syslog.socket
unit_regex:
 - lvm2-.*
 - cloud-.*
`)
	err := check.Configure(rawInstanceConfig, []byte(``))

	assert.Nil(t, err)
	// assert.Equal(t, true, check.config.instance.UnitNames)
	assert.ElementsMatch(t, []string{"ssh.service", "syslog.socket"}, check.config.instance.UnitNames)
	regexes := []*regexp.Regexp{
		regexp.MustCompile("lvm2-.*"),
		regexp.MustCompile("cloud-.*"),
	}
	assert.Equal(t, regexes, check.config.instance.UnitRegexPatterns)
}
func TestConfigurationSkipOnRegexError(t *testing.T) {
	// setup data
	check := Check{}
	rawInstanceConfig := []byte(`
unit_regex:
 - lvm2-.*
 - cloud-[[$$.*
 - abc
`)
	check.Configure(rawInstanceConfig, []byte(``))

	regexes := []*regexp.Regexp{
		regexp.MustCompile("lvm2-.*"),
		regexp.MustCompile("abc"),
	}
	assert.Equal(t, regexes, check.config.instance.UnitRegexPatterns)
}
func TestOverallMetrics(t *testing.T) {
	// setup data
	stats := &mockSystemdStats{}
	stats.On("ListUnits", mock.Anything).Return([]dbus.UnitStatus{
		{Name: "unit1.service", ActiveState: "active", SubState: "my_substate"},
		{Name: "unit2.service", ActiveState: "active", SubState: "my_substate"},
		{Name: "unit3.service", ActiveState: "inactive", SubState: "my_substate"},
	}, nil)

	stats.On("GetUnitTypeProperties", mock.Anything, mock.Anything, unitTypeService).Return(map[string]interface{}{
		"ActiveEnterTimestamp": uint64(999),
	}, nil)

	check := Check{stats: stats}
	check.Configure(nil, nil)

	// setup expectations
	mockSender := mocksender.NewMockSender(check.ID())
	mockSender.On("Gauge", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()
	mockSender.On("Commit").Return()

	// run
	check.Run()

	// asssertions
	mockSender.AssertCalled(t, "Gauge", "systemd.unit.active.count", float64(2), "", []string(nil))
	mockSender.AssertCalled(t, "Gauge", "systemd.unit.count", float64(1), "", []string{"unit:unit1.service", "active_state:active", "sub_state:my_substate"})
	mockSender.AssertCalled(t, "Gauge", "systemd.unit.count", float64(1), "", []string{"unit:unit2.service", "active_state:active", "sub_state:my_substate"})
	mockSender.AssertCalled(t, "Gauge", "systemd.unit.count", float64(1), "", []string{"unit:unit3.service", "active_state:inactive", "sub_state:my_substate"})
	mockSender.AssertNumberOfCalls(t, "Gauge", 4)
	mockSender.AssertNumberOfCalls(t, "Commit", 1)
}

func TestMonitoredUnitsDeclaredInConfig(t *testing.T) {
	// setup data
	rawInstanceConfig := []byte(`
unit_names:
 - unit1.service
 - unit2.service
`)

	stats := &mockSystemdStats{}
	stats.On("ListUnits", mock.Anything).Return([]dbus.UnitStatus{
		{Name: "unit1.service", ActiveState: "active", SubState: "my_substate"},
		{Name: "unit2.service", ActiveState: "active", SubState: "my_substate"},
		{Name: "unit3.service", ActiveState: "inactive", SubState: "my_substate"},
	}, nil)
	stats.On("TimeNanoNow").Return(int64(1000 * 1000))

	stats.On("GetUnitTypeProperties", mock.Anything, "unit1.service", unitTypeService).Return(map[string]interface{}{
		"CPUUsageNSec":  uint64(10),
		"MemoryCurrent": uint64(20),
		"TasksCurrent":  uint64(30),
	}, nil)

	stats.On("GetUnitTypeProperties", mock.Anything, "unit2.service", unitTypeService).Return(map[string]interface{}{
		"CPUUsageNSec":  uint64(110),
		"MemoryCurrent": uint64(120),
		"TasksCurrent":  uint64(130),
	}, nil)

	stats.On("GetUnitTypeProperties", mock.Anything, "unit1.service", unitTypeUnit).Return(map[string]interface{}{
		"ActiveEnterTimestamp": uint64(100),
	}, nil)
	stats.On("GetUnitTypeProperties", mock.Anything, "unit2.service", unitTypeUnit).Return(map[string]interface{}{
		"ActiveEnterTimestamp": uint64(200),
	}, nil)
	stats.On("GetUnitTypeProperties", mock.Anything, "unit3.service", unitTypeUnit).Return(map[string]interface{}{
		"ActiveEnterTimestamp": uint64(300),
	}, nil)

	check := Check{stats: stats}
	check.Configure(rawInstanceConfig, nil)

	// setup expectation
	mockSender := mocksender.NewMockSender(check.ID())
	mockSender.On("Gauge", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()
	mockSender.On("ServiceCheck", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()

	mockSender.On("Commit").Return()

	// run
	check.Run()

	// asssertions
	unit1Tags := []string{"unit:unit1.service", "active_state:active", "sub_state:my_substate"}
	mockSender.AssertCalled(t, "Gauge", "systemd.unit.count", float64(1), "", unit1Tags)
	mockSender.AssertCalled(t, "Gauge", "systemd.unit.cpu", float64(10), "", unit1Tags)
	mockSender.AssertCalled(t, "Gauge", "systemd.unit.memory", float64(20), "", unit1Tags)
	mockSender.AssertCalled(t, "Gauge", "systemd.unit.tasks", float64(30), "", unit1Tags)
	mockSender.AssertCalled(t, "Gauge", "systemd.unit.uptime", float64(900), "", unit1Tags)

	unit2Tags := []string{"unit:unit2.service", "active_state:active", "sub_state:my_substate"}
	mockSender.AssertCalled(t, "Gauge", "systemd.unit.count", float64(1), "", unit2Tags)
	mockSender.AssertCalled(t, "Gauge", "systemd.unit.cpu", float64(110), "", unit2Tags)
	mockSender.AssertCalled(t, "Gauge", "systemd.unit.memory", float64(120), "", unit2Tags)
	mockSender.AssertCalled(t, "Gauge", "systemd.unit.tasks", float64(130), "", unit2Tags)
	mockSender.AssertCalled(t, "Gauge", "systemd.unit.uptime", float64(800), "", unit2Tags)

	unit3Tags := []string{"unit:unit3.service", "active_state:inactive", "sub_state:my_substate"}
	mockSender.AssertCalled(t, "Gauge", "systemd.unit.count", float64(1), "", unit3Tags)
	mockSender.AssertNotCalled(t, "Gauge", "systemd.unit.cpu", mock.Anything, "", unit3Tags)

	expectedGaugeCalls := 3 * 2 /* 3 units * 2 metrics */
	expectedGaugeCalls += 2 * 3 /* 2 service * 3 metrics */
	mockSender.AssertNumberOfCalls(t, "Gauge", expectedGaugeCalls)
	mockSender.AssertNumberOfCalls(t, "Commit", 1)
}

func TestMonitoredUnitsServiceCheck(t *testing.T) {
	// setup data
	rawInstanceConfig := []byte(`
unit_names:
 - unit1.service
 - unit2.service
`)

	stats := &mockSystemdStats{}
	stats.On("ListUnits", mock.Anything).Return([]dbus.UnitStatus{
		{Name: "unit1.service", ActiveState: "active", SubState: "my_substate"},
		{Name: "unit2.service", ActiveState: "inactive", SubState: "my_substate"},
	}, nil)
	stats.On("TimeNanoNow").Return(int64(1000 * 1000))

	stats.On("GetUnitTypeProperties", mock.Anything, mock.Anything, unitTypeService).Return(map[string]interface{}{
		"CPUUsageNSec":  uint64(1),
		"MemoryCurrent": uint64(1),
		"TasksCurrent":  uint64(1),
	}, nil)

	stats.On("GetUnitTypeProperties", mock.Anything, "unit1.service", unitTypeUnit).Return(map[string]interface{}{
		"ActiveEnterTimestamp": uint64(100),
	}, nil)
	stats.On("GetUnitTypeProperties", mock.Anything, "unit2.service", unitTypeUnit).Return(map[string]interface{}{
		"ActiveEnterTimestamp": uint64(200),
	}, nil)

	check := Check{stats: stats}
	check.Configure(rawInstanceConfig, nil)

	// setup expectation
	mockSender := mocksender.NewMockSender(check.ID())
	mockSender.On("Gauge", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()
	mockSender.On("ServiceCheck", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()
	mockSender.On("Commit").Return()

	// run
	check.Run()

	// asssertions
	unit1Tags := []string{"unit:unit1.service", "active_state:active", "sub_state:my_substate"}
	mockSender.AssertCalled(t, "ServiceCheck", "systemd.unit.status", metrics.ServiceCheckOK, "", unit1Tags, "")

	unit2Tags := []string{"unit:unit2.service", "active_state:inactive", "sub_state:my_substate"}
	mockSender.AssertCalled(t, "ServiceCheck", "systemd.unit.status", metrics.ServiceCheckCritical, "", unit2Tags, "")

	mockSender.AssertNumberOfCalls(t, "ServiceCheck", 2)
	mockSender.AssertNumberOfCalls(t, "Commit", 1)
}

func TestGetServiceCheckStatus(t *testing.T) {
	data := []struct {
		activeState    string
		expectedStatus metrics.ServiceCheckStatus
	}{
		{"active", metrics.ServiceCheckOK},
		{"inactive", metrics.ServiceCheckCritical},
		{"failed", metrics.ServiceCheckCritical},
		{"activating", metrics.ServiceCheckUnknown},
		{"deactivating", metrics.ServiceCheckUnknown},
		{"does not exist", metrics.ServiceCheckUnknown},
	}
	for _, d := range data {
		t.Run(fmt.Sprintf("expected mapping from %s to %s", d.activeState, d.expectedStatus), func(t *testing.T) {
			assert.Equal(t, d.expectedStatus, getServiceCheckStatus(d.activeState))
		})
	}
}

func TestIsMonitored(t *testing.T) {
	// setup data
	rawInstanceConfig := []byte(`
unit_names:
  - unit1.service
  - unit2.service
unit_regex:
  - docker-.*
  - abc 
  - ^efg
  - ^zyz$
`)

	check := Check{}
	check.Configure(rawInstanceConfig, nil)

	data := []struct {
		unitName              string
		expectedToBeMonitored bool
	}{
		{"unit1.service", true},
		{"unit2.service", true},
		{"unit3.service", false},
		{"mydocker-abc.service", true},
		{"docker-abc.service", true},
		{"docker-123.socket", true},
		{"abc", true},
		{"abcd", true},
		{"xxabcd", true},
		{"efg111", true},
		{"z_efg111", false},
	}
	for _, d := range data {
		t.Run(fmt.Sprintf("check.isMonitored('%s') expected to be %v", d.unitName, d.expectedToBeMonitored), func(t *testing.T) {
			assert.Equal(t, d.expectedToBeMonitored, check.isMonitored(d.unitName))
		})
	}
}
