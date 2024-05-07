package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/ceph/go-ceph/rados"
	"github.com/ceph/go-ceph/rbd"
)

// CephImage is a wrapper around rbd.Image with the image name elements
// accessible as fields
type CephImage struct {
	*rbd.Image
	IOCtx *rados.IOContext

	Name           string
	Pool           string
	RadosNamespace string
}

func (c *CephImage) GetFullName() string {
	var name string

	if c.RadosNamespace != "" {
		name = fmt.Sprintf("%s/%s/%s", c.Pool, c.RadosNamespace, c.Name)
	} else {
		name = fmt.Sprintf("%s/%s", c.Pool, c.Name)
	}
	return name
}

func NewCephImage(name string, ctx *rados.IOContext) (*CephImage, error) {
	img := &CephImage{IOCtx: ctx}

	nameSlice := strings.Split(name, "/")
	if len(nameSlice) == 3 {
		img.Pool = nameSlice[0]
		img.RadosNamespace = nameSlice[1]
		img.Name = nameSlice[2]
	} else if len(nameSlice) == 2 {
		img.Pool = nameSlice[0]
		img.Name = nameSlice[1]
	} else {
		return nil, fmt.Errorf("ope")
	}

	return img, nil
}

// GetRadosConn establishes a RADOS connection
func GetRadosConn() (*rados.Conn, error) {
	conn, err := rados.NewConn()
	if err != nil {
		return nil, err
	}
	err = conn.ReadDefaultConfigFile()
	if err != nil {
		return nil, err
	}
	timeout := time.After(time.Second * 15)
	ch := make(chan error)
	go func(conn *rados.Conn) {
		ch <- conn.Connect()
	}(conn)
	select {
	case err = <-ch:
	case <-timeout:
		err = fmt.Errorf("timed out waiting for connect")
	}
	if err != nil {
		return nil, err
	}

	return conn, nil
}

// PhaseDataMigration does the thing
func PhaseDataMigration(source, target string, statusChan chan<- string) error {
	defer close(statusChan)

	statusChan <- "Establishing RADOS connection..."
	conn, err := GetRadosConn()
	if err != nil {
		return err
	}
	defer conn.Shutdown()

	statusChan <- fmt.Sprintf("Opening IO context for %q...", source)
	sourceCtx, err := conn.OpenIOContext(source)
	if err != nil {
		return err
	}
	defer sourceCtx.Destroy()

	statusChan <- fmt.Sprintf("Opening IO context for %q...", target)
	targetCtx, err := conn.OpenIOContext(target)
	if err != nil {
		return err
	}
	defer targetCtx.Destroy()

	statusChan <- fmt.Sprintf("Fetching image names from %q...", source)
	sourceImageNames, err := rbd.GetImageNames(sourceCtx)
	if err != nil {
		return err
	}

	for _, sourceImageName := range sourceImageNames {
		sourceImage, err := NewCephImage(source+"/"+sourceImageName, sourceCtx)
		if err != nil {
			return err
		}

		targetImage, err := NewCephImage(target+"/"+sourceImageName, targetCtx)
		if err != nil {
			return err
		}

		statusChan <- fmt.Sprintf("Migrating image data from %q to %q...", sourceImage.GetFullName(), targetImage.GetFullName())
		err = MigrateRbdImage(sourceImage, targetImage, statusChan)
		if err != nil {
			return err
		}
	}

	return nil
}

func MigrateRbdImage(sourceImage, targetImage *CephImage, statusChan chan<- string) error {
	var err error

	sourceImageName := sourceImage.GetFullName()
	sourceCtx := sourceImage.IOCtx

	targetImageName := targetImage.GetFullName()
	targetCtx := targetImage.IOCtx

	var status *rbd.MigrationImageStatus
	err = rbd.MigrationPrepare(sourceCtx, sourceImageName, targetCtx, targetImageName, rbd.NewRbdImageOptions())
	if err != nil {
		return err
	}

	status, err = rbd.MigrationStatus(targetCtx, targetImageName)
	if err != nil {
		return err
	} else if status.State != rbd.MigrationImagePrepared {
		return fmt.Errorf("failed migration prepare: %s", status.StateDescription)
	}
	statusChan <- fmt.Sprintf("%q migration state: %s", targetImageName, status.StateDescription)

	err = rbd.MigrationExecute(targetCtx, targetImageName)
	if err != nil {
		return err
	}

	for status.State != rbd.MigrationImageExecuted {
		status, err = rbd.MigrationStatus(targetCtx, targetImageName)
		if err != nil {
			return err
		}

		switch status.State {
		case rbd.MigrationImageAborting:
			fallthrough
		case rbd.MigrationImageError:
			return fmt.Errorf(status.StateDescription)
		default:
			statusChan <- fmt.Sprintf("%q migration state: %s", targetImageName, status.StateDescription)
			time.Sleep(time.Second * 5)
		}
	}

	err = rbd.MigrationCommit(targetCtx, targetImageName)
	if err != nil {
		return err
	}

	_, err = rbd.OpenImage(targetCtx, targetImageName, rbd.NoSnapshot)
	if err != nil {
		return err
	}

	_, err = rbd.OpenImage(sourceCtx, sourceImageName, rbd.NoSnapshot)
	if err == nil {
		return fmt.Errorf("image still there")
	} else if err != rbd.RbdErrorNotFound {
		return err
	}

	return nil
}
