package main

import (
	"fmt"
	"sync"
	"testing"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/newrelic/infra-integrations-sdk/data/metric"
	"github.com/newrelic/infra-integrations-sdk/integration"
	"github.com/newrelic/infra-integrations-sdk/persist"
)

func TestCollectMetrics(t *testing.T) {
	fmt.Println("Creating integration")
	i, err := integration.New("oracletest", "0.0.1")
	if err != nil {
		t.Error(err)
	}

	fmt.Println("Creating mock db")
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Error(err)
	}

	mock.MatchExpectationsInOrder(false)

	columns := []string{"INST_ID", "PhysicalReads", "PhysicalWrites", "PhysicalBlockReads", "PhysicalBlockWrites", "ReadTime", "WriteTime"}
	mock.ExpectQuery(`.*PHYRDS.*`).WillReturnRows(
		sqlmock.NewRows(columns).AddRow("1", 12, 23, 34, 45, 56, 67),
	)

	columns = []string{"INST_ID", "NAME", "VALUE"}
	mock.ExpectQuery(`.*pgastat.*`).WillReturnRows(
		sqlmock.NewRows(columns).AddRow("1", "total PGA inuse", 135),
	)

	columns = []string{"INST_ID", "METRIC_NAME", "VALUE"}
	mock.ExpectQuery(`.*sysmetric.*`).WillReturnRows(
		sqlmock.NewRows(columns).AddRow("1", "Buffer Cache Hit Ratio", 0.5),
	)

	columns = []string{"TABLESPACE_NAME", "USED", "OFFLINE", "SIZE", "USED_PERCENT"}
	mock.ExpectQuery(`.*TABLESPACE_NAME.*`).WillReturnRows(
		sqlmock.NewRows(columns).AddRow("testtablespace", 11, 0, 123, 12),
	)

	var populaterWg sync.WaitGroup
	populaterWg.Add(1)
	fmt.Println("Starting metrics collection")
	go collectMetrics(db, &populaterWg, i)
	populaterWg.Wait()

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Error(err)
	}

}

func TestGetOrCreateMetricSet(t *testing.T) {
	testCases := []struct {
		inputEntityID     string
		inputEntityType   string
		inputMap          map[string]*metric.Set
		expectedMetricSet string
	}{
		{
			inputEntityID:   "1",
			inputEntityType: "instance",
			inputMap: map[string]*metric.Set{
				"1": metric.NewSet("NewEventSample", persist.NewInMemoryStore()),
			},
			expectedMetricSet: `{"event_type":"NewEventSample"}`,
		},
		{
			inputEntityID:     "1",
			inputEntityType:   "instance",
			inputMap:          map[string]*metric.Set{},
			expectedMetricSet: `{"displayName":"instance1","entityName":"instance:instance1","event_type":"OracleDatabaseSample"}`,
		},
		{
			inputEntityID:     "testtablespace",
			inputEntityType:   "tablespace",
			inputMap:          map[string]*metric.Set{},
			expectedMetricSet: `{"displayName":"testtablespace","entityName":"tablespace:testtablespace","event_type":"OracleTablespaceSample"}`,
		},
	}

	i, _ := integration.New("oracletest", "0.0.1")
	for _, tc := range testCases {
		ms := getOrCreateMetricSet(tc.inputEntityID, tc.inputEntityType, tc.inputMap, i)
		marshalled, err := ms.MarshalJSON()
		if err != nil {
			t.Error(err)
		}

		if string(marshalled) != tc.expectedMetricSet {
			t.Errorf("Expected metric set %s, got %s", string(marshalled), tc.expectedMetricSet)
		}
	}
}

func TestPopulateMetrics(t *testing.T) {
	testCases := []struct {
		inputMetric  newrelicMetricSender
		expectedJSON string
	}{
		{
			inputMetric: newrelicMetricSender{
				metric: &newrelicMetric{
					name:       "testmetric",
					metricType: metric.GAUGE,
					value:      123.0,
				},
				metadata: map[string]string{
					"tablespace": "testtbname",
				},
			},
			expectedJSON: `{"name":"oracletest","protocol_version":"2","integration_version":"0.0.1","data":[{"entity":{"name":"testtbname","type":"tablespace"},"metrics":[{"displayName":"testtbname","entityName":"tablespace:testtbname","event_type":"OracleTablespaceSample","testmetric":123}],"inventory":{},"events":[]}]}`,
		},
		{
			inputMetric: newrelicMetricSender{
				metric: &newrelicMetric{
					name:       "testmetric",
					metricType: metric.ATTRIBUTE,
					value:      "testattr",
				},
				metadata: map[string]string{
					"instanceID": "1",
				},
			},
			expectedJSON: `{"name":"oracletest","protocol_version":"2","integration_version":"0.0.1","data":[{"entity":{"name":"1","type":"instance"},"metrics":[{"displayName":"instance1","entityName":"instance:instance1","event_type":"OracleDatabaseSample","testmetric":"testattr"}],"inventory":{},"events":[]}]}`,
		},
	}

	for _, tc := range testCases {
		i, _ := integration.New("oracletest", "0.0.1")
		var wg sync.WaitGroup
		metricChan := make(chan newrelicMetricSender)
		wg.Add(1)
		go populateMetrics(metricChan, &wg, i)

		metricChan <- tc.inputMetric
		close(metricChan)

		wg.Wait()

		marshalled, err := i.MarshalJSON()
		if err != nil {
			t.Error(err)
		}

		if string(marshalled) != tc.expectedJSON {
			t.Errorf("Expected %s, got %s", tc.expectedJSON, marshalled)
		}

	}
}