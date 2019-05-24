// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-2019 Datadog, Inc.
// +build !windows

package systemd

import (
	"regexp"
	"testing"

	"github.com/DataDog/datadog-agent/pkg/aggregator/mocksender"
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
func TestOverallMetrics(t *testing.T) {
	// create an instance of our test object
	stats := &mockSystemdStats{}
	stats.On("ListUnits", mock.Anything).Return([]dbus.UnitStatus{
		{Name: "unit1.service", ActiveState: "active"},
		{Name: "unit2.service", ActiveState: "active"},
		{Name: "unit3.service", ActiveState: "inactive"},
	}, nil)

	stats.On("GetUnitTypeProperties", mock.Anything, mock.Anything, mock.Anything).Return(map[string]interface{}{
		"ActiveState":          "active",
		"CPUUsageNSec":         uint64(10),
		"MemoryCurrent":        uint64(20),
		"TasksCurrent":         uint64(30),
		"ActiveEnterTimestamp": uint64(40),
	}, nil)

	check := Check{stats: stats}

	check.Configure(nil, nil)

	// setup expectations
	mockSender := mocksender.NewMockSender(check.ID())
	mockSender.On("Gauge", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()
	mockSender.On("Commit").Return()

	check.Run()

	mockSender.AssertCalled(t, "Gauge", "systemd.unit.active.count", float64(2), "", []string(nil))
	mockSender.AssertCalled(t, "Gauge", "systemd.unit.count", float64(1), "", []string{"unit:unit1.service", "active_state:active"})
	mockSender.AssertCalled(t, "Gauge", "systemd.unit.count", float64(1), "", []string{"unit:unit2.service", "active_state:active"})
	mockSender.AssertCalled(t, "Gauge", "systemd.unit.count", float64(1), "", []string{"unit:unit3.service", "active_state:inactive"})
	mockSender.AssertNumberOfCalls(t, "Gauge", 4)
	mockSender.AssertNumberOfCalls(t, "Commit", 1)
}

func TestMonitoredUnitsDeclaredInConfig(t *testing.T) {
	// timeNanoNow = func() int64 { return 1000 * 1000 }

	rawInstanceConfig := []byte(`
unit_names:
 - unit1.service
 - unit2.service
`)

	// create an instance of our test object
	stats := &mockSystemdStats{}
	stats.On("ListUnits", mock.Anything).Return([]dbus.UnitStatus{
		{Name: "unit1.service", ActiveState: "active"},
		{Name: "unit2.service", ActiveState: "active"},
		{Name: "unit3.service", ActiveState: "inactive"},
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
		"ActiveState":          "active",
		"ActiveEnterTimestamp": uint64(100),
	}, nil)
	stats.On("GetUnitTypeProperties", mock.Anything, "unit2.service", unitTypeUnit).Return(map[string]interface{}{
		"ActiveState":          "active",
		"ActiveEnterTimestamp": uint64(200),
	}, nil)
	stats.On("GetUnitTypeProperties", mock.Anything, "unit3.service", unitTypeUnit).Return(map[string]interface{}{
		"ActiveState":          "active",
		"ActiveEnterTimestamp": uint64(300),
	}, nil)

	check := Check{stats: stats}

	check.Configure(rawInstanceConfig, nil)

	// setup expectations
	mockSender := mocksender.NewMockSender(check.ID())
	mockSender.On("Gauge", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()
	mockSender.On("Commit").Return()

	check.Run()

	unit1Tags := []string{"unit:unit1.service", "active_state:active"}
	mockSender.AssertCalled(t, "Gauge", "systemd.unit.count", float64(1), "", unit1Tags)
	mockSender.AssertCalled(t, "Gauge", "systemd.unit.cpu", float64(10), "", unit1Tags)
	mockSender.AssertCalled(t, "Gauge", "systemd.unit.memory", float64(20), "", unit1Tags)
	mockSender.AssertCalled(t, "Gauge", "systemd.unit.tasks", float64(30), "", unit1Tags)
	mockSender.AssertCalled(t, "Gauge", "systemd.unit.uptime", float64(900), "", unit1Tags)

	unit2Tags := []string{"unit:unit2.service", "active_state:active"}
	mockSender.AssertCalled(t, "Gauge", "systemd.unit.count", float64(1), "", unit2Tags)
	mockSender.AssertCalled(t, "Gauge", "systemd.unit.cpu", float64(110), "", unit2Tags)
	mockSender.AssertCalled(t, "Gauge", "systemd.unit.memory", float64(120), "", unit2Tags)
	mockSender.AssertCalled(t, "Gauge", "systemd.unit.tasks", float64(130), "", unit2Tags)
	mockSender.AssertCalled(t, "Gauge", "systemd.unit.uptime", float64(800), "", unit2Tags)
	mockSender.AssertNumberOfCalls(t, "Commit", 1)
}
