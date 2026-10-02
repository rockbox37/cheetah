package main

import "context"

type Plan struct {
	Name                string   `json:"name"`
	MaxCreditsPerMonth  int      `json:"max_credits_per_month"`
	AllowedTiers        []string `json:"allowed_tiers"`
	CanExtract          bool     `json:"can_extract"`
	MaxRatePerMinute    int      `json:"max_rate_per_min"`
	MaxConcurrency      int      `json:"max_concurrency"`
	MaxCrawlPages       int      `json:"max_crawl_pages"`
	ResultRetentionHours int     `json:"result_retention_hours"`
	CanWebhook          bool     `json:"can_webhook"`
	HasSLA              bool     `json:"has_sla"`
	CanOverage          bool     `json:"can_overage"`
	MaxBandwidthGB      int      `json:"max_bandwidth_gb"`
}

var FreePlan = Plan{
	Name:                 "free",
	MaxCreditsPerMonth:   500,
	AllowedTiers:         []string{"fast"},
	MaxRatePerMinute:     10,
	MaxConcurrency:       2,
	MaxCrawlPages:        10,
	ResultRetentionHours: 1,
	MaxBandwidthGB:       1,
}

// PlanLoader resolves an owner hash to its subscription plan.
// ownerHash is the SHA-256 hex digest of the API key.
type PlanLoader interface {
	LoadPlan(ctx context.Context, ownerHash string) (Plan, error)
}

// UsageReporter records credit consumption after a request completes.
// ownerHash is the SHA-256 of the API key, not the raw credential.
type UsageReporter interface {
	ReportUsage(ctx context.Context, ownerHash string, credits int, responseBytes int64, success bool)
}

type FreePlanLoader struct{}

func (FreePlanLoader) LoadPlan(_ context.Context, _ string) (Plan, error) {
	return FreePlan, nil
}

type NoOpUsageReporter struct{}

func (NoOpUsageReporter) ReportUsage(_ context.Context, _ string, _ int, _ int64, _ bool) {}

type AppOption func(*appDeps)

type appDeps struct {
	planLoader    PlanLoader
	usageReporter UsageReporter
}

func defaultDeps() appDeps {
	return appDeps{
		planLoader:    FreePlanLoader{},
		usageReporter: NoOpUsageReporter{},
	}
}

func WithPlanLoader(pl PlanLoader) AppOption {
	return func(d *appDeps) { d.planLoader = pl }
}

func WithUsageReporter(ur UsageReporter) AppOption {
	return func(d *appDeps) { d.usageReporter = ur }
}
