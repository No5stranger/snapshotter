package main

import (
	"fmt"
	snapshotsapi "github.com/containerd/containerd/api/services/snapshots/v1"
	"github.com/containerd/containerd/contrib/snapshotservice"
	"github.com/containerd/containerd/snapshots/overlay"
	"google.golang.org/grpc"
	"net"
	"os"
	"os/signal"
	"syscall"
)

var (
	snapshotterSocketPath = "/run/containerd/a-overlayfs"
	overlayfsRootPath     = "/var/lib/containerd/a-overlayfs/"
)

func main() {
	rpc := grpc.NewServer()

	sn, err := overlay.NewSnapshotter(overlayfsRootPath)
	if err != nil {
		fmt.Printf("NewSnapshotter error: %s", err)
		os.Exit(1)
	}

	service := snapshotservice.FromSnapshotter(sn)
	snapshotsapi.RegisterSnapshotsServer(rpc, service)

	sock, err := net.Listen("unix", snapshotterSocketPath)
	if err != nil {
		fmt.Printf("OpenSocket error: %s", err)
		os.Exit(1)
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		os.Remove(snapshotterSocketPath)
		os.Exit(0)
	}()

	if err := rpc.Serve(sock); err != nil {
		fmt.Printf("GRPC Server error: %s", err)
		os.Exit(1)
	}
}
