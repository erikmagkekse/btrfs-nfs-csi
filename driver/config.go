package driver

import (
	"errors"
	"time"
)

const (
	staleCheckTimeout = 5 * time.Second
	apiCallTimeout    = 10 * time.Second

	eventMountHealthy       = "MountHealthy"
	eventMountRemounted     = "MountRemounted"
	eventMountRemountFailed = "MountRemountFailed"
)

var errStatTimeout = errors.New("stat timed out (likely stale NFS mount)")
