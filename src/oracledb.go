package main

import (
	"fmt"
	"strings"
	"sync"

	"github.com/jmoiron/sqlx"
	sdkArgs "github.com/newrelic/infra-integrations-sdk/args"
	"github.com/newrelic/infra-integrations-sdk/integration"
	"github.com/newrelic/infra-integrations-sdk/log"
	goracle "gopkg.in/goracle.v2"
)

type argumentList struct {
	sdkArgs.DefaultArgumentList
	ServiceName     string `default:"" help:"The Oracle service name"`
	Username        string `default:"" help:"The OracleDB connection user name"`
	Password        string `default:"" help:"The OracleDB connection password"`
	IsSysDBA        bool   `default:"false" help:"Is the user a SysDBA"`
	IsSysOper       bool   `default:"false" help:"Is the user a SysOper"`
	Hostname        string `default:"127.0.0.1" help:"The OracleDB connection host name"`
	Port            string `default:"1521" help:"The OracleDB connection port"`
	ExtendedMetrics bool   `default:"false" help:"Enable extended metrics"`
}

const (
	integrationName    = "com.newrelic.oracledb"
	integrationVersion = "0.1.0"
)

var (
	args   argumentList
	logger log.Logger
)

func main() {
	// Create Integration
	i, err := integration.New(integrationName, integrationVersion, integration.Args(&args))
	panicOnErr(err)

	logger = log.NewStdErr(args.Verbose)

	sid := fmt.Sprintf("%s:%s/%s", args.Hostname, args.Port, args.ServiceName)
	cp := goracle.ConnectionParams{
		Username:    args.Username,
		Password:    args.Password,
		SID:         sid,
		IsSysDBA:    args.IsSysDBA,
		IsSysOper:   args.IsSysOper,
		MaxSessions: 8,
	}

	// TODO when new version is released, change String to StringWithPassword
	connString := strings.Replace(cp.String(), "SECRET", cp.Password, 1)
	db, err := sqlx.Open("goracle", connString)
	panicOnErr(err)

	err = db.Ping()
	panicOnErr(err)

	var workerWg sync.WaitGroup
	if args.All() || args.Metrics {

		var wg sync.WaitGroup
		metricChan := make(chan newRelicMetricSender)

		wg.Add(4)
		go oracleReadWriteMetrics.Collect(db, &wg, metricChan)
		go oraclePgaMetrics.Collect(db, &wg, metricChan)
		go oracleSysMetrics.Collect(db, &wg, metricChan)
		go oracleTablespaceMetrics.Collect(db, &wg, metricChan)

		go func() {
			wg.Wait()
			close(metricChan)
		}()

		workerWg.Add(1)
		metricsWorker(metricChan, &workerWg, i)
	}

	if args.All() || args.Inventory {
		workerWg.Add(1)
		inventoryWorker(db, &workerWg, i)
	}

	workerWg.Wait()

	panicOnErr(i.Publish())
}

func panicOnErr(err error) {
	if err != nil {
		panic(err)
	}
}
