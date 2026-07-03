package cmd

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/dublyobase/dublyobase/apis"
	"github.com/spf13/cobra"
)

func newServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the API server and admin UI",
		RunE: func(cmd *cobra.Command, args []string) error {
			srv := apis.NewServer(app)

			// Graceful shutdown: stop the HTTP server and any supervised clusters.
			ctx, stop := signal.NotifyContext(context.Background(),
				syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			go func() {
				<-ctx.Done()
				log.Println("shutting down...")
				shutCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				_ = srv.Shutdown(shutCtx)
				if err := app.Supervisor.StopAll(); err != nil {
					log.Println("cluster shutdown:", err)
				}
			}()

			log.Printf("dublyobase serving on http://%s (data dir: %s)",
				app.Settings.HTTPAddr, app.Settings.DataDir)

			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				return err
			}
			return nil
		},
		SilenceErrors: false,
	}
}
