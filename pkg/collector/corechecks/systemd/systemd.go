// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-2019 Datadog, Inc.

package systemd

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/DataDog/datadog-agent/pkg/aggregator"
	"github.com/DataDog/datadog-agent/pkg/autodiscovery/integration"
	"github.com/DataDog/datadog-agent/pkg/collector/check"
	"github.com/DataDog/datadog-agent/pkg/metrics"
	"github.com/DataDog/datadog-agent/pkg/util/log"
	"github.com/coreos/go-systemd/dbus"
	"gopkg.in/yaml.v2"

	core "github.com/DataDog/datadog-agent/pkg/collector/corechecks"
)

const (
	systemdCheckName   = "systemd"
	unitTag            = "unit"
	unitActiveStateTag = "active_state"
	unitSubStateTag    = "sub_state"
	unitActiveState    = "active"
	unitTypeUnit       = "Unit"
	unitTypeService    = "Service"
	serviceSuffix      = "service"
)

// Check doesn't need additional fields
type Check struct {
	core.CheckBase
	stats  systemdStats
	config systemdConfig
}

type systemdInstanceConfig struct {
	UnitNames         []string `yaml:"unit_names"`
	UnitRegexStrings  []string `yaml:"unit_regex"`
	UnitRegexPatterns []*regexp.Regexp
}

type systemdInitConfig struct{}

type systemdConfig struct {
	instance systemdInstanceConfig
	initConf systemdInitConfig
}

type systemdStats interface {
	// Dbus Connection
	NewConn() (*dbus.Conn, error)
	CloseConn(c *dbus.Conn)

	// System Data
	ListUnits(c *dbus.Conn) ([]dbus.UnitStatus, error)
	GetUnitTypeProperties(c *dbus.Conn, unitName string, unitType string) (map[string]interface{}, error)

	// misc
	TimeNanoNow() int64
}

type defaultSystemdStats struct{}

func (s *defaultSystemdStats) NewConn() (*dbus.Conn, error) {
	return dbus.New()
}

func (s *defaultSystemdStats) CloseConn(c *dbus.Conn) {
	c.Close()
}

func (s *defaultSystemdStats) ListUnits(conn *dbus.Conn) ([]dbus.UnitStatus, error) {
	return conn.ListUnits()
}

func (s *defaultSystemdStats) TimeNanoNow() int64 {
	return time.Now().UnixNano()
}

func (s *defaultSystemdStats) GetUnitTypeProperties(c *dbus.Conn, unitName string, unitType string) (map[string]interface{}, error) {
	return c.GetUnitTypeProperties(unitName, unitType)
}

// Run executes the check
func (c *Check) Run() error {
	sender, err := aggregator.GetSender(c.ID())
	if err != nil {
		// TODO: test this case
		return err
	}

	conn, err := c.stats.NewConn()
	if err != nil {
		log.Error("New Connection Err: ", err)
		// TODO: test this case
		return err
	}
	defer c.stats.CloseConn(conn)
	// TODO: CHECK conn.SystemState()

	c.submitUnitMetrics(sender, conn)

	sender.Commit()
	return nil
}

func (c *Check) submitUnitMetrics(sender aggregator.Sender, conn *dbus.Conn) {
	log.Info("Check Unit Metrics")
	units, err := c.stats.ListUnits(conn)
	if err != nil {
		log.Errorf("Error getting list of units")
		// TODO: test this case
		return
	}

	activeUnitCounter := 0
	for _, unit := range units {
		if unit.ActiveState == unitActiveState {
			activeUnitCounter++
		}

		tags := []string{
			unitTag + ":" + unit.Name,
			unitActiveStateTag + ":" + unit.ActiveState,
			unitSubStateTag + ":" + unit.SubState,
		}

		sender.Gauge("systemd.unit.count", 1, "", tags)

		if c.isMonitored(unit.Name) {
			c.submitMonitoredUnitMetrics(sender, conn, unit, tags)
			if strings.HasSuffix(unit.Name, "."+serviceSuffix) {
				c.submitMonitoredServiceMetrics(sender, conn, unit, tags)
			}
		}
	}

	sender.Gauge("systemd.unit.active.count", float64(activeUnitCounter), "", nil)
}

func (c *Check) submitMonitoredUnitMetrics(sender aggregator.Sender, conn *dbus.Conn, unit dbus.UnitStatus, tags []string) {
	unitProperties, err := c.stats.GetUnitTypeProperties(conn, unit.Name, unitTypeUnit)
	if err != nil {
		log.Errorf("Error getting unit unitProperties: %s", unit.Name)
		// TODO: test this case
		return
	}

	activeEnterTime, err := getNumberProperty(unitProperties, "ActiveEnterTimestamp")
	if err != nil {
		log.Errorf("Error getting property ActiveEnterTimestamp: %v", err)
		// TODO: test this dase
		return
	}
	sender.Gauge("systemd.unit.uptime", float64(getUptime(activeEnterTime, c.stats.TimeNanoNow())), "", tags)
	sender.ServiceCheck("systemd.unit.status", getServiceCheckStatus(unit.ActiveState), "", tags, "")
}

func (c *Check) submitMonitoredServiceMetrics(sender aggregator.Sender, conn *dbus.Conn, unit dbus.UnitStatus, tags []string) {
	serviceProperties, err := c.stats.GetUnitTypeProperties(conn, unit.Name, unitTypeService)
	if err != nil {
		log.Errorf("Error getting serviceProperties for service: %s", unit.Name)
		// TODO: test this case
		return
	}

	sendPropertyAsGauge(sender, serviceProperties, "CPUUsageNSec", "systemd.unit.cpu", tags)
	sendPropertyAsGauge(sender, serviceProperties, "MemoryCurrent", "systemd.unit.memory", tags)
	sendPropertyAsGauge(sender, serviceProperties, "TasksCurrent", "systemd.unit.tasks", tags)
}

func sendPropertyAsGauge(sender aggregator.Sender, properties map[string]interface{}, propertyName string, metric string, tags []string) {
	value, err := getNumberProperty(properties, propertyName)
	if err != nil {
		log.Errorf("Error getting property %s: %v", propertyName, err)
		// TODO: test this dase
		return
	}
	sender.Gauge(metric, float64(value), "", tags)
}

func getUptime(activeEnterTime uint64, nanoNow int64) int64 {
	uptime := nanoNow/1000 - int64(activeEnterTime)
	return uptime
}

func getNumberProperty(properties map[string]interface{}, propertyName string) (uint64, error) {
	value, ok := properties[propertyName].(uint64)
	if !ok {
		// TODO: test this case
		return 0, fmt.Errorf("Property %s is not a uint64", propertyName)
	}
	return value, nil
}

func getStringProperty(properties map[string]interface{}, propertyName string) (string, error) {
	value, ok := properties[propertyName].(string)
	if !ok {
		// TODO: test this case
		return "", fmt.Errorf("Property %s is not a string", propertyName)
	}
	return value, nil
}

func getServiceCheckStatus(activeState string) metrics.ServiceCheckStatus {
	switch activeState {
	case "active":
		return metrics.ServiceCheckOK
	case "inactive", "failed":
		return metrics.ServiceCheckCritical
	case "activating", "deactivating":
		return metrics.ServiceCheckUnknown
	}
	return metrics.ServiceCheckUnknown
}

func (c *Check) isMonitored(unitName string) bool {
	for _, name := range c.config.instance.UnitNames {
		if name == unitName {
			return true
		}
	}
	return false
}

// Configure configures the systemd checks
func (c *Check) Configure(rawInstance integration.Data, rawInitConfig integration.Data) error {
	err := c.CommonConfigure(rawInstance)
	if err != nil {
		// TODO: test this case
		return err
	}
	err = yaml.Unmarshal(rawInitConfig, &c.config.initConf)
	if err != nil {
		// TODO: test this case
		return err
	}
	err = yaml.Unmarshal(rawInstance, &c.config.instance)
	if err != nil {
		// TODO: test this case
		return err
	}

	for _, regexString := range c.config.instance.UnitRegexStrings {
		pattern, err := regexp.Compile(regexString)
		if err != nil {
			log.Errorf("Failed to parse systemd check option unit_regex: %s", err)
			// TODO: test this case
			continue
		}
		c.config.instance.UnitRegexPatterns = append(c.config.instance.UnitRegexPatterns, pattern)
	}
	return nil
}

func systemdFactory() check.Check {
	return &Check{
		stats:     &defaultSystemdStats{},
		CheckBase: core.NewCheckBase(systemdCheckName),
	}
}

func init() {
	core.RegisterCheck(systemdCheckName, systemdFactory)
}
