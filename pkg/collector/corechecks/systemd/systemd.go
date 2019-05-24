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
	"github.com/DataDog/datadog-agent/pkg/util/log"
	"github.com/coreos/go-systemd/dbus"
	"gopkg.in/yaml.v2"

	core "github.com/DataDog/datadog-agent/pkg/collector/corechecks"
)

const systemdCheckName = "systemd"

const (
	unitActiveState = "active"
)

// For testing purpose
var (
	dbusNew       = dbus.New
	connListUnits = func(c *dbus.Conn) ([]dbus.UnitStatus, error) { return c.ListUnits() }
	connClose     = func(c *dbus.Conn) { c.Close() }
	timeNow       = time.Now
)

// SystemdCheck doesn't need additional fields
type SystemdCheck struct {
	core.CheckBase
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

// Run executes the check
func (c *SystemdCheck) Run() error {

	log.Warnf("[DEV] c.config.instance.UnitNames Run: %v", c.config.instance.UnitNames)
	log.Warnf("[DEV] c.config.instance.UnitRegexStrings Run: %v", c.config.instance.UnitRegexStrings)
	log.Warnf("[DEV] c.config.instance.UnitRegexPatterns Run: %v", c.config.instance.UnitRegexPatterns)

	sender, err := aggregator.GetSender(c.ID())
	if err != nil {
		return err
	}

	conn, err := dbusNew()
	if err != nil {
		log.Error("New Connection Err: ", err)
		return err
	}
	defer connClose(conn)

	submitOverallMetrics(sender, conn)

	for _, unitName := range c.config.instance.UnitNames {
		tags := []string{"unit:" + unitName}
		c.submitUnitMetrics(sender, conn, unitName, tags)
		if strings.HasSuffix(unitName, ".service") {
			c.submitServiceMetrics(sender, conn, unitName, tags)
		}
	}

	sender.Commit()

	return nil
}

func (c *SystemdCheck) submitUnitMetrics(sender aggregator.Sender, conn *dbus.Conn, unitName string, tags []string) {
	log.Infof("[DEV] Check Unit %s", unitName)

	properties, err := conn.GetUnitProperties(unitName)
	if err != nil {
		log.Errorf("Error getting unit properties: %s", unitName)
	}

	log.Infof("Unit Properties len: %v", len(properties))
	log.Infof("Unit Properties len: %v", properties)

	activeState, err := getStringProperty(properties, "ActiveState")
	if err != nil {
		log.Errorf("Error getting property: %s", err)
	} else {
		tags = append(tags, "active_state:"+activeState)
	}

	sender.Gauge("systemd.unit.count", 1, "", tags)

	activeEnterTime, err := getNumberProperty(properties, "ActiveEnterTimestamp") // microseconds
	if err != nil {
		log.Errorf("Error getting property ActiveEnterTimestamp: %v", err)
	} else {
		sender.Gauge("systemd.unit.uptime", float64(getUptime(activeEnterTime, timeNow().UnixNano())), "", tags) // microseconds
	}
}

func getUptime(activeEnterTime uint64, nanoNow int64) int64 {
	log.Infof("activeEnterTime: %v %T", activeEnterTime, activeEnterTime)
	log.Infof("nanoNow: %v %T", nanoNow, nanoNow)
	uptime := nanoNow/1000 - int64(activeEnterTime)
	log.Infof("uptime: %v", uptime)
	log.Infof("uptime mins: %v", uptime/1000000/60)
	return uptime
}

func submitPropertyAsGauge(sender aggregator.Sender, properties map[string]interface{}, propertyName string, metric string, tags []string) {
	value, err := getNumberProperty(properties, propertyName)
	if err != nil {
		log.Errorf("Error getting property %s: %v", propertyName, err)
		return
	}

	sender.Gauge(metric, float64(value), "", tags)
}

func getNumberProperty(properties map[string]interface{}, propertyName string) (uint64, error) {
	log.Infof("[DEV] properties[propertyName]: %s", properties[propertyName])
	value, ok := properties[propertyName].(uint64)
	if !ok {
		return 0, fmt.Errorf("Property %s is not a uint64", propertyName)
	}

	log.Infof("[DEV] Get Number Property %s = %d", propertyName, value)
	return value, nil
}

func getStringProperty(properties map[string]interface{}, propertyName string) (string, error) {
	value, ok := properties[propertyName].(string)
	if !ok {
		return "", fmt.Errorf("Property %s is not a string", propertyName)
	}

	log.Infof("[DEV] Get String Property %s = %s", propertyName, value)
	return value, nil
}

func (c *SystemdCheck) submitServiceMetrics(sender aggregator.Sender, conn *dbus.Conn, unitName string, tags []string) {
	properties, err := conn.GetUnitTypeProperties(unitName, "Service") // TODO: change me
	if err != nil {
		log.Errorf("Error getting properties for service: %s", unitName)
	}

	submitPropertyAsGauge(sender, properties, "CPUUsageNSec", "systemd.unit.cpu", tags)
	submitPropertyAsGauge(sender, properties, "MemoryCurrent", "systemd.unit.memory", tags)
	submitPropertyAsGauge(sender, properties, "TasksCurrent", "systemd.unit.tasks", tags)
}

func submitOverallMetrics(sender aggregator.Sender, conn *dbus.Conn) {
	log.Info("Check Overall Metrics")
	units, err := connListUnits(conn)
	if err != nil {
		log.Errorf("Error getting list of units")
	}

	activeUnitCounter := 0
	for _, unit := range units {
		log.Info("[DEV] [unit] %s: ActiveState=%s, SubState=%s", unit.Name, unit.ActiveState, unit.SubState)
		if unit.ActiveState == unitActiveState {
			activeUnitCounter++
		}
	}

	sender.Gauge("systemd.unit.active.count", float64(activeUnitCounter), "", nil)
}

// Configure configures the network checks
func (c *SystemdCheck) Configure(rawInstance integration.Data, rawInitConfig integration.Data) error {
	err := c.CommonConfigure(rawInstance)
	if err != nil {
		return err
	}
	err = yaml.Unmarshal(rawInitConfig, &c.config.initConf)
	if err != nil {
		return err
	}
	err = yaml.Unmarshal(rawInstance, &c.config.instance)
	if err != nil {
		return err
	}

	log.Warnf("[DEV] c.config.instance.UnitNames: %v", c.config.instance.UnitNames)
	log.Warnf("[DEV] c.config.instance.UnitRegexStrings: %v", c.config.instance.UnitRegexStrings)

	for _, regexString := range c.config.instance.UnitRegexStrings {
		pattern, err := regexp.Compile(regexString)
		if err != nil {
			log.Errorf("Failed to parse systemd check option unit_regex: %s", err)
		} else {
			c.config.instance.UnitRegexPatterns = append(c.config.instance.UnitRegexPatterns, pattern)
		}
	}
	log.Warnf("[DEV] c.config.instance.UnitRegexPatterns: %v", c.config.instance.UnitRegexPatterns)
	return nil
}

func systemdFactory() check.Check {
	return &SystemdCheck{
		CheckBase: core.NewCheckBase(systemdCheckName),
	}
}

func init() {
	core.RegisterCheck(systemdCheckName, systemdFactory)
}
