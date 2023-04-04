package main

import (
	"context"
	"fmt"
	"log"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/snapshots"
)

func main() {
	client, err := containerd.New("/run/containerd/containerd.sock", containerd.WithDefaultNamespace("default"))
	if err != nil {
		log.Fatal("containerd create failed")
	}
	ctx := context.Background()
	image, err := client.Pull(ctx, "docker.io/library/nginx:latest",
		containerd.WithPullUnpack,
		containerd.WithPullSnapshotter("a-overlayfs",
			snapshots.WithLabels(map[string]string{"containerd.io/snapshot.ref": "ng_snap_hack_ref"})))
	if err != nil {
		log.Fatal("pull image failed, error: ", err)
	}
	fmt.Printf("containerd image pulled, %s", image.Name())
}
