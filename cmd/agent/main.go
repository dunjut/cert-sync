package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"k8s.io/apiserver/pkg/util/logs"

	"github.com/dunjut/cert-sync/pkg/agent"
)

var (
	opts agent.InitOptions

	rootCmd = &cobra.Command{
		Use:          "agent",
		Short:        "cert-sync agent",
		Long:         "cert-sync agent automatically synchronize TLS Secret from Kubernetes to specified certificate directory.",
		SilenceUsage: true,
	}

	syncCmd = &cobra.Command{
		Use:   "sync",
		Short: "Start cert-sync service",
		RunE: func(*cobra.Command, []string) error {
			logs.InitLogs()
			defer logs.FlushLogs()

			agent := new(agent.Agent)
			agent.Initialize(opts)

			stopCh := setupSignalHandler()
			agent.Run(stopCh)
			return nil
		},
	}
)

func init() {
	syncCmd.PersistentFlags().StringVar(&opts.CertDir, "certDir", "",
		"Directory where certificates will be placed into")
	cobra.MarkFlagRequired(
		syncCmd.PersistentFlags(),
		"certDir",
	)
	syncCmd.PersistentFlags().StringVar(&opts.KubeConfig, "kubeconfig", "",
		"Path to kubeconfig file")
	syncCmd.PersistentFlags().IntVar(&opts.Thread, "thread", 0,
		fmt.Sprintf("Number of worker threads (%d-%d), default %d",
			agent.MinThreadiness, agent.MaxThreadiness, agent.DefaultThreadiness),
	)

	rootCmd.AddCommand(syncCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(-1)
	}
}

// setupSignalHandler registered for SIGTERM and SIGINT. A stop channel is returned
// which is closed on one of these signals. If a second signal is caught, the program
// is terminated with exit code 1.
func setupSignalHandler() (stopCh <-chan struct{}) {
	stop := make(chan struct{})
	sigs := make(chan os.Signal, 2)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		close(stop)
		<-sigs
		os.Exit(1) // second signal. Exit directly.
	}()

	return stop
}
