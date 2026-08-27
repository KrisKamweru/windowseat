package contracts

import "time"

type Component string

const (
	ComponentService   Component = "service"
	ComponentTray      Component = "tray"
	ComponentWake      Component = "wake_helper"
	ComponentMCPBridge Component = "mcp_bridge"
)

type ComponentState string

const (
	ComponentHealthy      ComponentState = "healthy"
	ComponentDegraded     ComponentState = "degraded"
	ComponentUnavailable  ComponentState = "unavailable"
	ComponentDisabled     ComponentState = "disabled"
	ComponentNotInstalled ComponentState = "not_installed"
)

type CheckState string

const (
	CheckPass    CheckState = "pass"
	CheckWarning CheckState = "warning"
	CheckFail    CheckState = "fail"
	CheckUnknown CheckState = "unknown"
)

type ProviderOperationalState string

const (
	ProviderOperationalHealthy     ProviderOperationalState = "healthy"
	ProviderOperationalDegraded    ProviderOperationalState = "degraded"
	ProviderOperationalUnavailable ProviderOperationalState = "unavailable"
	ProviderOperationalDisabled    ProviderOperationalState = "disabled"
)

type HealthReport struct {
	SchemaVersion string            `json:"schema_version"`
	ObservedAt    time.Time         `json:"observed_at"`
	Live          bool              `json:"live"`
	Ready         bool              `json:"ready"`
	Service       ComponentHealth   `json:"service"`
	Providers     []ProviderHealth  `json:"providers"`
	Components    []ComponentHealth `json:"components,omitempty"`
}

type ComponentHealth struct {
	Component Component      `json:"component"`
	State     ComponentState `json:"state"`
	Version   string         `json:"version,omitempty"`
	StartedAt *time.Time     `json:"started_at,omitempty"`
	Checks    []HealthCheck  `json:"checks,omitempty"`
}

type ProviderHealth struct {
	Provider             Provider                 `json:"provider"`
	State                ProviderState            `json:"state"`
	OperationalState     ProviderOperationalState `json:"operational_state"`
	CheckedAt            time.Time                `json:"checked_at"`
	Source               *Source                  `json:"source,omitempty"`
	LastSuccessfulReadAt *time.Time               `json:"last_successful_read_at,omitempty"`
	LastError            *ProviderError           `json:"last_error,omitempty"`
	Checks               []HealthCheck            `json:"checks,omitempty"`
}

type HealthCheck struct {
	Name       string     `json:"name"`
	State      CheckState `json:"state"`
	CheckedAt  time.Time  `json:"checked_at"`
	Code       string     `json:"code,omitempty"`
	Message    string     `json:"message,omitempty"`
	DurationMS *float64   `json:"duration_ms,omitempty"`
}
