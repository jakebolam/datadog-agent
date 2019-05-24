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

type Conn struct {
}

func dbusNewFake() (*dbus.Conn, error) {
	return nil, nil
}

func connListUnitsFake(c *dbus.Conn) ([]dbus.UnitStatus, error) {
	return []dbus.UnitStatus{
		{Name: "unit1.service", ActiveState: "active"},
		{Name: "unit2.service", ActiveState: "active"},
		{Name: "unit3.service", ActiveState: "inactive"},
	}, nil
}

func connCloseFake(c *dbus.Conn) {
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
	dbusNew = dbusNewFake
	connListUnits = connListUnitsFake
	connClose = connCloseFake
	connGetUnitTypeProperties = func(c *dbus.Conn, unitName string, unitType string) (map[string]interface{}, error) {
		return map[string]interface{}{
			"ActiveState":          "active",
			"CPUUsageNSec":         uint64(10),
			"MemoryCurrent":        uint64(20),
			"TasksCurrent":         uint64(30),
			"ActiveEnterTimestamp": uint64(40),
		}, nil
	}

	// create an instance of our test object
	check := new(Check)
	check.Configure(nil, nil)

	// setup expectations
	mockSender := mocksender.NewMockSender(check.ID())
	mockSender.On("Gauge", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return()
	mockSender.On("Commit").Return()

	check.Run()

	mockSender.AssertCalled(t, "Gauge", "systemd.unit.active.count", float64(2), "", []string(nil))
	mockSender.AssertCalled(t, "Gauge", "systemd.unit.count", float64(1), "", []string{"unit:unit1.service", "active_state:active"})
	mockSender.AssertCalled(t, "Gauge", "systemd.unit.count", float64(1), "", []string{"unit:unit2.service", "active_state:active"})
	mockSender.AssertCalled(t, "Gauge", "systemd.unit.count", float64(1), "", []string{"unit:unit3.service", "active_state:active"})
	mockSender.AssertNumberOfCalls(t, "Gauge", 4)
	mockSender.AssertNumberOfCalls(t, "Commit", 1)
}

func TestMonitoredUnitsDeclaredInConfig(t *testing.T) {
	dbusNew = dbusNewFake
	connListUnits = connListUnitsFake
	connClose = connCloseFake
	connGetUnitTypeProperties = func(c *dbus.Conn, unitName string, unitType string) (map[string]interface{}, error) {
		switch unitType {
		case "Service":
			switch unitName {
			case "unit1.service":
				return map[string]interface{}{
					"CPUUsageNSec":  uint64(10),
					"MemoryCurrent": uint64(20),
					"TasksCurrent":  uint64(30),
				}, nil
			case "unit2.service":
				return map[string]interface{}{
					"CPUUsageNSec":  uint64(110),
					"MemoryCurrent": uint64(120),
					"TasksCurrent":  uint64(130),
				}, nil
			}
		case "Unit":
			switch unitName {
			case "unit1.service":
				return map[string]interface{}{
					"ActiveState":          "active",
					"ActiveEnterTimestamp": uint64(100),
				}, nil
			case "unit2.service":
				return map[string]interface{}{
					"ActiveState":          "active",
					"ActiveEnterTimestamp": uint64(200),
				}, nil
			case "unit3.service":
				return map[string]interface{}{
					"ActiveState":          "active",
					"ActiveEnterTimestamp": uint64(300),
				}, nil
			}
		}
		return nil, nil
	}
	timeNanoNow = func() int64 { return 1000 * 1000 }

	rawInstanceConfig := []byte(`
unit_names:
 - unit1.service
 - unit2.service
`)
	// create an instance of our test object
	check := new(Check)
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
