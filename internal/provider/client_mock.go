package provider

import (
	"context"

	"github.com/stretchr/testify/mock"

	"github.com/dash0hq/terraform-provider-dash0/internal/provider/client"
)

// MockClient mocks the client.Client interface
type MockClient struct {
	mock.Mock
}

func (m *MockClient) CreateDashboard(ctx context.Context, origin string, dashboardJSON string, dataset string) error {
	args := m.Called(ctx, origin, dashboardJSON, dataset)
	return args.Error(0)
}

func (m *MockClient) GetDashboard(ctx context.Context, origin string, dataset string) (string, error) {
	args := m.Called(ctx, origin, dataset)
	return args.String(0), args.Error(1)
}

func (m *MockClient) UpdateDashboard(ctx context.Context, origin string, dashboardJSON string, dataset string) error {
	args := m.Called(ctx, origin, dashboardJSON, dataset)
	return args.Error(0)
}

func (m *MockClient) DeleteDashboard(ctx context.Context, origin string, dataset string) error {
	args := m.Called(ctx, origin, dataset)
	return args.Error(0)
}

func (m *MockClient) ResolveDashboard(ctx context.Context, origin string, dataset string) (string, string, error) {
	args := m.Called(ctx, origin, dataset)
	return args.String(0), args.String(1), args.Error(2)
}

func (m *MockClient) CreateSyntheticCheck(ctx context.Context, origin string, checkJSON string, dataset string) error {
	args := m.Called(ctx, origin, checkJSON, dataset)
	return args.Error(0)
}

func (m *MockClient) GetSyntheticCheck(ctx context.Context, origin string, dataset string) (string, error) {
	args := m.Called(ctx, origin, dataset)
	return args.String(0), args.Error(1)
}

func (m *MockClient) UpdateSyntheticCheck(ctx context.Context, origin string, checkJSON string, dataset string) error {
	args := m.Called(ctx, origin, checkJSON, dataset)
	return args.Error(0)
}

func (m *MockClient) DeleteSyntheticCheck(ctx context.Context, origin string, dataset string) error {
	args := m.Called(ctx, origin, dataset)
	return args.Error(0)
}

func (m *MockClient) ResolveSyntheticCheck(ctx context.Context, origin string, dataset string) (string, string, error) {
	args := m.Called(ctx, origin, dataset)
	return args.String(0), args.String(1), args.Error(2)
}

func (m *MockClient) CreateView(ctx context.Context, origin string, viewJSON string, dataset string) error {
	args := m.Called(ctx, origin, viewJSON, dataset)
	return args.Error(0)
}

func (m *MockClient) GetView(ctx context.Context, origin string, dataset string) (string, error) {
	args := m.Called(ctx, origin, dataset)
	return args.String(0), args.Error(1)
}

func (m *MockClient) UpdateView(ctx context.Context, origin string, viewJSON string, dataset string) error {
	args := m.Called(ctx, origin, viewJSON, dataset)
	return args.Error(0)
}

func (m *MockClient) DeleteView(ctx context.Context, origin string, dataset string) error {
	args := m.Called(ctx, origin, dataset)
	return args.Error(0)
}

func (m *MockClient) ResolveView(ctx context.Context, origin string, dataset string) (string, string, error) {
	args := m.Called(ctx, origin, dataset)
	return args.String(0), args.String(1), args.Error(2)
}

func (m *MockClient) CreateCheckRule(ctx context.Context, origin string, ruleYAML string, dataset string) error {
	args := m.Called(ctx, origin, ruleYAML, dataset)
	return args.Error(0)
}

func (m *MockClient) GetCheckRule(ctx context.Context, origin string, dataset string) (string, error) {
	args := m.Called(ctx, origin, dataset)
	return args.String(0), args.Error(1)
}

func (m *MockClient) UpdateCheckRule(ctx context.Context, origin string, ruleYAML string, dataset string) error {
	args := m.Called(ctx, origin, ruleYAML, dataset)
	return args.Error(0)
}

func (m *MockClient) DeleteCheckRule(ctx context.Context, origin string, dataset string) error {
	args := m.Called(ctx, origin, dataset)
	return args.Error(0)
}

func (m *MockClient) ResolveCheckRule(ctx context.Context, origin string, dataset string) (string, string, error) {
	args := m.Called(ctx, origin, dataset)
	return args.String(0), args.String(1), args.Error(2)
}

func (m *MockClient) CreateRecordingRule(ctx context.Context, origin string, ruleJSON string, dataset string) error {
	args := m.Called(ctx, origin, ruleJSON, dataset)
	return args.Error(0)
}

func (m *MockClient) GetRecordingRule(ctx context.Context, origin string, dataset string) (string, error) {
	args := m.Called(ctx, origin, dataset)
	return args.String(0), args.Error(1)
}

func (m *MockClient) UpdateRecordingRule(ctx context.Context, origin string, ruleJSON string, dataset string) error {
	args := m.Called(ctx, origin, ruleJSON, dataset)
	return args.Error(0)
}

func (m *MockClient) DeleteRecordingRule(ctx context.Context, origin string, dataset string) error {
	args := m.Called(ctx, origin, dataset)
	return args.Error(0)
}

func (m *MockClient) ResolveRecordingRule(ctx context.Context, origin string, dataset string) (string, error) {
	args := m.Called(ctx, origin, dataset)
	return args.String(0), args.Error(1)
}

func (m *MockClient) CreateNotificationChannel(ctx context.Context, origin string, channelJSON string) error {
	args := m.Called(ctx, origin, channelJSON)
	return args.Error(0)
}

func (m *MockClient) GetNotificationChannel(ctx context.Context, origin string) (string, error) {
	args := m.Called(ctx, origin)
	return args.String(0), args.Error(1)
}

func (m *MockClient) UpdateNotificationChannel(ctx context.Context, origin string, channelJSON string) error {
	args := m.Called(ctx, origin, channelJSON)
	return args.Error(0)
}

func (m *MockClient) DeleteNotificationChannel(ctx context.Context, origin string) error {
	args := m.Called(ctx, origin)
	return args.Error(0)
}

func (m *MockClient) ResolveNotificationChannel(ctx context.Context, origin string) (string, string, error) {
	args := m.Called(ctx, origin)
	return args.String(0), args.String(1), args.Error(2)
}

func (m *MockClient) CreateDataset(ctx context.Context, origin string, datasetJSON string) (string, error) {
	args := m.Called(ctx, origin, datasetJSON)
	return args.String(0), args.Error(1)
}

func (m *MockClient) GetDataset(ctx context.Context, slug string) (string, error) {
	args := m.Called(ctx, slug)
	return args.String(0), args.Error(1)
}

func (m *MockClient) UpdateDataset(ctx context.Context, slug string, origin string, datasetJSON string) error {
	args := m.Called(ctx, slug, origin, datasetJSON)
	return args.Error(0)
}

func (m *MockClient) DeleteDataset(ctx context.Context, slug string) error {
	args := m.Called(ctx, slug)
	return args.Error(0)
}

func (m *MockClient) CreateAuthToken(ctx context.Context, origin string, authTokenJSON string) (string, string, error) {
	args := m.Called(ctx, origin, authTokenJSON)
	return args.String(0), args.String(1), args.Error(2)
}

func (m *MockClient) GetAuthToken(ctx context.Context, idOrOrigin string) (string, string, error) {
	args := m.Called(ctx, idOrOrigin)
	return args.String(0), args.String(1), args.Error(2)
}

func (m *MockClient) UpdateAuthToken(ctx context.Context, idOrOrigin string, origin string, authTokenJSON string) error {
	args := m.Called(ctx, idOrOrigin, origin, authTokenJSON)
	return args.Error(0)
}

func (m *MockClient) DeleteAuthToken(ctx context.Context, idOrOrigin string) error {
	args := m.Called(ctx, idOrOrigin)
	return args.Error(0)
}

func (m *MockClient) CreateTeam(ctx context.Context, origin string, teamJSON string) error {
	args := m.Called(ctx, origin, teamJSON)
	return args.Error(0)
}

func (m *MockClient) GetTeam(ctx context.Context, origin string) (string, error) {
	args := m.Called(ctx, origin)
	return args.String(0), args.Error(1)
}

func (m *MockClient) UpdateTeam(ctx context.Context, origin string, teamJSON string) error {
	args := m.Called(ctx, origin, teamJSON)
	return args.Error(0)
}

func (m *MockClient) DeleteTeam(ctx context.Context, origin string) error {
	args := m.Called(ctx, origin)
	return args.Error(0)
}

func (m *MockClient) ResolveTeam(ctx context.Context, origin string) (string, error) {
	args := m.Called(ctx, origin)
	return args.String(0), args.Error(1)
}

func (m *MockClient) CreateSpamFilter(ctx context.Context, origin string, filterJSON string, dataset string) error {
	args := m.Called(ctx, origin, filterJSON, dataset)
	return args.Error(0)
}

func (m *MockClient) GetSpamFilter(ctx context.Context, origin string, dataset string) (string, error) {
	args := m.Called(ctx, origin, dataset)
	return args.String(0), args.Error(1)
}

func (m *MockClient) UpdateSpamFilter(ctx context.Context, origin string, filterJSON string, dataset string) error {
	args := m.Called(ctx, origin, filterJSON, dataset)
	return args.Error(0)
}

func (m *MockClient) DeleteSpamFilter(ctx context.Context, origin string, dataset string) error {
	args := m.Called(ctx, origin, dataset)
	return args.Error(0)
}

func (m *MockClient) ResolveSpamFilter(ctx context.Context, origin string, dataset string) (string, error) {
	args := m.Called(ctx, origin, dataset)
	return args.String(0), args.Error(1)
}

func (m *MockClient) GetAwsMetricScrapeConfig(ctx context.Context, integrationID string) (string, string, error) {
	args := m.Called(ctx, integrationID)
	return args.String(0), args.String(1), args.Error(2)
}

func (m *MockClient) UpdateAwsMetricScrapeConfig(ctx context.Context, integrationID string, origin string, cfgJSON string) error {
	args := m.Called(ctx, integrationID, origin, cfgJSON)
	return args.Error(0)
}

func (m *MockClient) DeleteAwsMetricScrapeConfig(ctx context.Context, integrationID string) error {
	args := m.Called(ctx, integrationID)
	return args.Error(0)
}

func (m *MockClient) CreateTimeSeriesAggregation(ctx context.Context, origin string, tsaJSON string, dataset string) error {
	args := m.Called(ctx, origin, tsaJSON, dataset)
	return args.Error(0)
}

func (m *MockClient) GetTimeSeriesAggregation(ctx context.Context, origin string, dataset string) (string, error) {
	args := m.Called(ctx, origin, dataset)
	return args.String(0), args.Error(1)
}

func (m *MockClient) UpdateTimeSeriesAggregation(ctx context.Context, origin string, tsaJSON string, dataset string) error {
	args := m.Called(ctx, origin, tsaJSON, dataset)
	return args.Error(0)
}

func (m *MockClient) DeleteTimeSeriesAggregation(ctx context.Context, origin string, dataset string) error {
	args := m.Called(ctx, origin, dataset)
	return args.Error(0)
}

func (m *MockClient) ResolveTimeSeriesAggregation(ctx context.Context, origin string, dataset string) (string, error) {
	args := m.Called(ctx, origin, dataset)
	return args.String(0), args.Error(1)
}

func (m *MockClient) CreateSamplingRule(ctx context.Context, origin string, ruleJSON string, dataset string) error {
	args := m.Called(ctx, origin, ruleJSON, dataset)
	return args.Error(0)
}

func (m *MockClient) GetSamplingRule(ctx context.Context, origin string, dataset string) (string, error) {
	args := m.Called(ctx, origin, dataset)
	return args.String(0), args.Error(1)
}

func (m *MockClient) UpdateSamplingRule(ctx context.Context, origin string, ruleJSON string, dataset string) error {
	args := m.Called(ctx, origin, ruleJSON, dataset)
	return args.Error(0)
}

func (m *MockClient) DeleteSamplingRule(ctx context.Context, origin string, dataset string) error {
	args := m.Called(ctx, origin, dataset)
	return args.Error(0)
}

func (m *MockClient) ResolveSamplingRule(ctx context.Context, origin string, dataset string) (string, error) {
	args := m.Called(ctx, origin, dataset)
	return args.String(0), args.Error(1)
}

func (m *MockClient) CreateSignalToMetrics(ctx context.Context, origin string, ruleJSON string, dataset string) error {
	args := m.Called(ctx, origin, ruleJSON, dataset)
	return args.Error(0)
}

func (m *MockClient) GetSignalToMetrics(ctx context.Context, origin string, dataset string) (string, error) {
	args := m.Called(ctx, origin, dataset)
	return args.String(0), args.Error(1)
}

func (m *MockClient) UpdateSignalToMetrics(ctx context.Context, origin string, ruleJSON string, dataset string) error {
	args := m.Called(ctx, origin, ruleJSON, dataset)
	return args.Error(0)
}

func (m *MockClient) DeleteSignalToMetrics(ctx context.Context, origin string, dataset string) error {
	args := m.Called(ctx, origin, dataset)
	return args.Error(0)
}

func (m *MockClient) ResolveSignalToMetrics(ctx context.Context, origin string, dataset string) (string, error) {
	args := m.Called(ctx, origin, dataset)
	return args.String(0), args.Error(1)
}

func (m *MockClient) SendLogEvent(ctx context.Context, event client.LogEvent, dataset string) error {
	args := m.Called(ctx, event, dataset)
	return args.Error(0)
}

func (m *MockClient) CreateSLO(ctx context.Context, origin string, sloJSON string, dataset string) error {
	args := m.Called(ctx, origin, sloJSON, dataset)
	return args.Error(0)
}

func (m *MockClient) GetSLO(ctx context.Context, origin string, dataset string) (string, error) {
	args := m.Called(ctx, origin, dataset)
	return args.String(0), args.Error(1)
}

func (m *MockClient) UpdateSLO(ctx context.Context, origin string, sloJSON string, dataset string) error {
	args := m.Called(ctx, origin, sloJSON, dataset)
	return args.Error(0)
}

func (m *MockClient) DeleteSLO(ctx context.Context, origin string, dataset string) error {
	args := m.Called(ctx, origin, dataset)
	return args.Error(0)
}

func (m *MockClient) ResolveSLO(ctx context.Context, origin string, dataset string) (string, string, error) {
	args := m.Called(ctx, origin, dataset)
	return args.String(0), args.String(1), args.Error(2)
}
