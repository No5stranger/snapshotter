package overlay

import (
	"context"
	"errors"
	"fmt"
	"github.com/containerd/containerd/errdefs"
	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/mount"
	"github.com/containerd/containerd/snapshots"
	"github.com/containerd/containerd/snapshots/storage"
	"github.com/containerd/continuity/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const (
	remoteSnapshotRoot  string = "/data/mnt/"
	targetSnapshotLabel        = "containerd.io/snapshot.ref"
	remoteSnapshotLabel        = "containerd.io/snapshot/remote"
	cacheSnapshotLabel         = "containerd.io/snapshot/image-cache"
)

type RemoteSnapshot struct {
	ms *storage.MetaStore
}

func NewRemoteSnapshot(dbfile string) (*RemoteSnapshot, error) {
	ms, err := storage.NewMetaStore(dbfile)
	if err != nil {
		return nil, err
	}
	return &RemoteSnapshot{ms: ms}, nil
}

func GetRemoteSnapshot(labels map[string]string) (*RemoteSnapshot, error) {
	imc, ok := labels[cacheSnapshotLabel]
	if !ok || len(imc) == 0 {
		return nil, nil
	}
	dbfile := filepath.Join(remoteSnapshotRoot, imc, "metadata.db")
	if _, err := os.Stat(dbfile); errors.Is(err, os.ErrNotExist) {
		return nil, os.ErrNotExist
	}
	return NewRemoteSnapshot(dbfile)
}

func (rs *RemoteSnapshot) Stat(ctx context.Context, key string) (id string, info snapshots.Info, err error) {
	if err := rs.ms.WithTransaction(ctx, false, func(ctx context.Context) error {
		id, info, _, err = storage.GetInfo(ctx, key)
		return err
	}); err != nil {
		fmt.Printf("Overlayfs|RemoteSnapshot,err:%s\n", err)
		return id, info, err
	}
	fmt.Printf("Overlayfs|RemoteSnapshot|Stat,key:%s,id:%s", key, id)
	return id, info, nil
}

func (rs *RemoteSnapshot) Check(ctx context.Context, target string) (id string, info snapshots.Info, err error) {
	checkFunc := func(ctx context.Context, i snapshots.Info) error {
		fmt.Printf("Overlayfs|RemoteSnapshot|checkFunc,info:%v\n", i)
		namePattern := strings.Split(i.Name, "/")
		if len(namePattern) >= 3 && namePattern[2] == target {
			id, info, _, err = storage.GetInfo(ctx, i.Name)
			if err != nil {
				return err
			}
		}
		return nil
	}
	err = rs.ms.WithTransaction(ctx, false, func(ctx context.Context) error {
		return storage.WalkInfo(ctx, checkFunc)
	})
	fmt.Printf("Overlayfs|RemoteSnapshot|Check,target:%s,id:%s,name:%s,parent:%s,err:%v\n", target, id, info.Name, info.Parent, err)
	return id, info, err
}

func (rs *RemoteSnapshot) Close(ctx context.Context) {
	err := rs.ms.Close()
	fmt.Printf("RemoteSnapshot|Close,err:%v", err)
}

func (o *snapshotter) PrepareV2(ctx context.Context, key, parent string, opts ...snapshots.Opt) ([]mount.Mount, error) {
	fmt.Printf("Overlayfs|PrepareV2,key:%s,parent:%s,opts:%d\n", key, parent, len(opts))
	s, err := o.createSnapshotV2(ctx, snapshots.KindActive, key, parent, opts)
	if err != nil {
		return nil, err
	}

	var base snapshots.Info
	for _, opt := range opts {
		if err := opt(&base); err != nil {
			return nil, err
		}
	}
	fmt.Printf("Overlayfs|PrepareV2,key:%s,parent:%s,label:%v\n", key, parent, base.Labels)
	if target, ok := base.Labels[targetSnapshotLabel]; ok {
		rs, err := GetRemoteSnapshot(base.Labels)
		if rs != nil && err == nil {
			defer rs.Close(ctx)
			id, info, err := rs.Check(ctx, target)
			fmt.Printf("Overlayfs|PrepareV2|ReadCache,target:%s,id:%s,info:%v,err:%v\n", target, id, info, err)
			idInt, _ := strconv.ParseInt(id, 10, 64)
			if idInt > 0 {
				base.Labels[remoteSnapshotLabel] = id
				opts = append(opts, snapshots.WithLabels(base.Labels))
				err = o.doCommit(ctx, target, key, true, opts...)
				if err != nil {
					return nil, err
				}
				return nil, errdefs.ErrAlreadyExists
			}

		} else {
			fmt.Printf("Overlayfs|PrepareV2|GetRemoteSnapshot,rs:%v,err:%v\n", rs, err)
		}
	}
	//return o.mounts(s), nil
	return o.doMounts(ctx, s, key)
}

func (o *snapshotter) createSnapshotV2(ctx context.Context, kind snapshots.Kind, key, parent string, opts []snapshots.Opt) (s storage.Snapshot, err error) {
	var (
		td, path string
	)

	defer func() {
		if err != nil {
			if td != "" {
				if err1 := os.RemoveAll(td); err1 != nil {
					log.G(ctx).WithError(err1).Warn("failed to cleanup temp snapshot directory")
				}
			}
			if path != "" {
				if err1 := os.RemoveAll(path); err1 != nil {
					log.G(ctx).WithError(err1).WithField("path", path).Error("failed to reclaim snapshot directory, directory may need removal")
					err = fmt.Errorf("failed to remove path: %v: %w", err1, err)
				}
			}
		}
	}()

	if err := o.ms.WithTransaction(ctx, true, func(ctx context.Context) (err error) {
		snapshotDir := filepath.Join(o.root, "snapshots")
		td, err = o.prepareDirectory(ctx, snapshotDir, kind)
		if err != nil {
			return fmt.Errorf("failed to create prepare snapshot dir: %w", err)
		}

		s, err = storage.CreateSnapshot(ctx, kind, key, parent, opts...)
		if err != nil {
			return fmt.Errorf("failed to create snapshot: %w", err)
		}

		if len(s.ParentIDs) > 0 {
			st, err := os.Stat(o.upperPath(s.ParentIDs[0]))
			if err != nil {
				return fmt.Errorf("failed to stat parent: %w", err)
			}

			stat := st.Sys().(*syscall.Stat_t)
			if err := os.Lchown(filepath.Join(td, "fs"), int(stat.Uid), int(stat.Gid)); err != nil {
				return fmt.Errorf("failed to chown: %w", err)
			}
		}

		path = filepath.Join(snapshotDir, s.ID)
		if err = os.Rename(td, path); err != nil {
			return fmt.Errorf("failed to rename: %w", err)
		}
		td = ""

		return nil
	}); err != nil {
		return storage.Snapshot{}, err
	}
	return s, nil
}

func (o *snapshotter) doCommit(ctx context.Context, name, key string, isRemote bool, opts ...snapshots.Opt) error {
	return o.ms.WithTransaction(ctx, true, func(ctx context.Context) error {
		id, _, usage, err := storage.GetInfo(ctx, key)
		if err != nil {
			return err
		}
		if !isRemote {
			du, err := fs.DiskUsage(ctx, o.upperPath(id))
			if err != nil {
				return err
			}
			usage = snapshots.Usage(du)
		}
		if _, err = storage.CommitActive(ctx, key, name, usage, opts...); err != nil {
			return fmt.Errorf("failed to commit snapshot %s: %w", key, err)
		}
		return nil
	})
}

func (o *snapshotter) doMounts(ctx context.Context, s storage.Snapshot, key string) ([]mount.Mount, error) {
	fmt.Printf("Overlayfs|doMounts,s:%#v\n", s)
	if len(s.ParentIDs) == 0 {
		// if we only have one layer/no parents then just return a bind mount as overlay
		// will not work
		roFlag := "rw"
		if s.Kind == snapshots.KindView {
			roFlag = "ro"
		}

		return []mount.Mount{
			{
				Source: o.upperPath(s.ID),
				Type:   "bind",
				Options: []string{
					roFlag,
					"rbind",
				},
			},
		}, nil
	}

	fakeParentPaths := make([]string, 0, len(s.ParentIDs))
	if err := o.ms.WithTransaction(ctx, false, func(ctx context.Context) error {
		for cKey := key; cKey != ""; {
			id, info, _, err := storage.GetInfo(ctx, cKey)
			fmt.Printf("Overlayfs|doMounts|GetInfo,id:%s,name:%s,labels:%v,err:%v\n", id, info.Name, info.Labels, err)
			if err != nil {
				return err
			}
			if targetID, ok := info.Labels[remoteSnapshotLabel]; ok {
				if imc, ok := info.Labels[cacheSnapshotLabel]; ok {
					fakeParentPaths = append(fakeParentPaths, o.fakeUpperPath(imc, targetID))
				} else {
					fmt.Printf("Overlayfs|doMounts|MissIMC,id:%v,info:%v\n", id, info)
				}
			}
			cKey = info.Parent
		}
		return nil
	}); err != nil {
		return nil, err
	}

	var options []string

	// set index=off when mount overlayfs
	if o.indexOff {
		options = append(options, "index=off")
	}

	if o.userxattr {
		options = append(options, "userxattr")
	}

	if s.Kind == snapshots.KindActive {
		options = append(options,
			fmt.Sprintf("workdir=%s", o.workPath(s.ID)),
			fmt.Sprintf("upperdir=%s", o.upperPath(s.ID)),
		)
	} else if len(fakeParentPaths) == 1 {
		return []mount.Mount{
			{
				Source: fakeParentPaths[0],
				Type:   "bind",
				Options: []string{
					"ro",
					"rbind",
				},
			},
		}, nil
	} else if len(s.ParentIDs) == 1 {
		return []mount.Mount{
			{
				Source: o.upperPath(s.ParentIDs[0]),
				Type:   "bind",
				Options: []string{
					"ro",
					"rbind",
				},
			},
		}, nil
	}

	parentPaths := make([]string, len(s.ParentIDs))
	if len(fakeParentPaths) > 0 {
		for i := range fakeParentPaths {
			parentPaths[i] = fakeParentPaths[i]
		}

	} else {
		for i := range s.ParentIDs {
			parentPaths[i] = o.upperPath(s.ParentIDs[i])
		}
	}

	options = append(options, fmt.Sprintf("lowerdir=%s", strings.Join(parentPaths, ":")))
	return []mount.Mount{
		{
			Type:    "overlay",
			Source:  "overlay",
			Options: options,
		},
	}, nil
}

func (o *snapshotter) fakeUpperPath(imc, id string) string {
	return filepath.Join(remoteSnapshotRoot, imc, "snapshots", id, "fs")
}
