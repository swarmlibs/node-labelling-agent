package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/alecthomas/kingpin/v2"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/oklog/run"
	"github.com/prometheus-operator/prometheus-operator/pkg/versionutil"
	"github.com/prometheus/common/version"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

var (
	serviceLabelPrefix        = "service."
	stackNamespaceLabelPrefix = "stack."
	dockerStackNamespaceLabel = "com.docker.stack.namespace"
	defaultRefreshInterval    = 15 * time.Second
)

func main() {
	app := kingpin.New("node-metadata-agent", "")

	dockerStackNamespace := app.Flag("stack.namespace", "Filter by stack namespace").Default("").String()
	refreshInterval := app.Flag("refresh.interval", "refresh interval").Default(defaultRefreshInterval.String()).Duration()

	var logger log.Logger
	logger = log.NewLogfmtLogger(os.Stdout)
	logger = level.NewFilter(logger, level.AllowAll())
	logger = log.With(logger, "ts", log.DefaultTimestampUTC)
	logger = log.With(logger, "caller", log.DefaultCaller)

	versionutil.RegisterIntoKingpinFlags(app)

	if versionutil.ShouldPrintVersion() {
		versionutil.Print(os.Stdout, "node-metadata-agent")
		os.Exit(0)
	}

	if _, err := app.Parse(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stdout, err)
		os.Exit(2)
	}

	level.Info(logger).Log("msg", "Starting node-metadata-agent", "version", version.Info())
	level.Info(logger).Log("build_context", version.BuildContext())

	var (
		g           run.Group
		ctx, cancel = context.WithCancel(context.Background())
	)

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		level.Error(logger).Log("msg", "Failed to create docker client", "err", err)
		os.Exit(1)
	}

	localNodeInfo, err := cli.Info(ctx)
	if err != nil {
		level.Error(logger).Log("msg", "Failed to get docker info", "err", err)
		os.Exit(1)
	}

	// Run the processTaskLabels function every refreshInterval
	quit := make(chan struct{})
	g.Add(func() error {
		ticker := time.NewTicker(*refreshInterval)
		for {
			select {
			case <-ticker.C:
				localNodeSpec, _, err := cli.NodeInspectWithRaw(ctx, localNodeInfo.Swarm.NodeID)

				if err != nil {
					level.Error(logger).Log("msg", "Failed to get node spec", "err", err)
					continue
				}

				if !localNodeSpec.ManagerStatus.Leader {
					level.Debug(logger).Log("msg", "Not a leader node, skipping node metadata processing")
					continue
				}

				discoveredNodes := map[string]map[string]string{}
				serviceFilter := filters.NewArgs()

				if dockerStackNamespace != nil {
					serviceFilter.Add("label", fmt.Sprintf("%s=%s", dockerStackNamespaceLabel, *dockerStackNamespace))
				}
				services, err := cli.ServiceList(ctx, types.ServiceListOptions{Filters: serviceFilter})
				if err != nil {
					level.Error(logger).Log("msg", "Failed to get service list", "err", err)
					continue
				}

				for _, service := range services {
					tasks, err := cli.TaskList(ctx, types.TaskListOptions{
						Filters: filters.NewArgs(filters.Arg("service", service.Spec.Name)),
					})
					if err != nil {
						level.Error(logger).Log("msg", "Failed to get task list", "err", err)
						continue
					}

					for _, task := range tasks {
						// Add the node ID to the discoveredNodes map
						if _, ok := discoveredNodes[task.NodeID]; !ok {
							discoveredNodes[task.NodeID] = map[string]string{}
						}

						// Add the stack namespace to the discoveredNodes map
						if _, ok := service.Spec.Labels[dockerStackNamespaceLabel]; ok {
							discoveredNodes[task.NodeID][fmt.Sprintf("%s%s", stackNamespaceLabelPrefix, service.Spec.Labels[dockerStackNamespaceLabel])] = "true"
						}

						// Add the service name to the discoveredNodes map
						discoveredNodes[task.NodeID][fmt.Sprintf("%s%s", serviceLabelPrefix, service.Spec.Name)] = "true"
					}
				}

				nodes, err := cli.NodeList(ctx, types.NodeListOptions{})
				if err != nil {
					level.Error(logger).Log("msg", "Failed to get node list", "err", err)
					continue
				}

				for _, node := range nodes {
					labels := map[string]string{}

					if _, ok := discoveredNodes[node.ID]; ok {
						level.Debug(logger).Log("msg", "Discovered labels for node", "node", node.ID, "labels", fmt.Sprintf("%v", discoveredNodes[node.ID]))
						labels = discoveredNodes[node.ID]
					}

					for key, value := range labels {
						if _, ok := node.Spec.Labels[key]; !ok {
							node.Spec.Labels[key] = value
							level.Debug(logger).Log("msg", "Adding label to node", "node", node.ID, "label", key, "value", value)
						}
					}

					for key := range node.Spec.Labels {
						if _, ok := labels[key]; !ok {
							if strings.HasPrefix(key, fmt.Sprintf("%s%s", serviceLabelPrefix, *dockerStackNamespace)) {
								delete(node.Spec.Labels, key)
								level.Debug(logger).Log("msg", "Removing label from node", "node", node.ID, "label", key)
							}
							if strings.HasPrefix(key, fmt.Sprintf("%s%s", stackNamespaceLabelPrefix, *dockerStackNamespace)) {
								delete(node.Spec.Labels, key)
								level.Debug(logger).Log("msg", "Removing label from node", "node", node.ID, "label", key)
							}
						}
					}

					err = cli.NodeUpdate(ctx, node.ID, node.Version, node.Spec)
					if err != nil {
						level.Error(logger).Log("msg", "Failed to update node labels", "node", node.ID, "err", err)
						continue
					}
				}
			case <-quit:
				ticker.Stop()
				return nil
			}
		}
	}, func(error) {
		cli.Close()
		cancel()
	})

	// Handle Interrupt & SIGTERM signals
	term := make(chan os.Signal, 1)
	signal.Notify(term, os.Interrupt, syscall.SIGTERM)
	g.Add(func() error {
		select {
		case <-term:
			close(quit)
			level.Info(logger).Log("msg", "Received SIGTERM, exiting gracefully...")
		case <-ctx.Done():
		}

		return nil
	}, func(error) {})

	if err := g.Run(); err != nil {
		level.Error(logger).Log("msg", "Failed to run", "err", err)
		os.Exit(1)
	}
}
