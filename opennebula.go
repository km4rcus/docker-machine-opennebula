package opennebula

import (
	"errors"
	"fmt"
	"io/ioutil"
	"strconv"
	"time"

	"github.com/OpenNebula/goca"
	"github.com/docker/machine/libmachine/drivers"
	"github.com/docker/machine/libmachine/log"
	"github.com/docker/machine/libmachine/mcnflag"
	"github.com/docker/machine/libmachine/ssh"
	"github.com/docker/machine/libmachine/state"
)

type Driver struct {
	*drivers.BaseDriver
	NetworkName     string
	NetworkOwner    string
	NetworkId       string
	CPU             string
	VCPU            string
	Memory          string
	DiskSize        string
	Boot2DockerURL  string
	DatastoreId     string
}

const (
	defaultTimeout        = 1 * time.Second
	defaultSSHUser        = "docker"
	defaultCPU            = "1"
	defaultVCPU           = ""
	defaultMemory         = "1024"
	defaultDiskSize       = "20000"
	defaultBoot2DockerURL = "https://s3.eu-central-1.amazonaws.com/one-boot2d/boot2docker-v1.9.1.iso"
	defaultDatastoreId    = "1"
)

func NewDriver(hostName, storePath string) *Driver {
	return &Driver{
		BaseDriver: &drivers.BaseDriver{
			SSHUser:     defaultSSHUser,
			MachineName: hostName,
			StorePath:   storePath,
		},
	}
}

// GetCreateFlags registers the flags this driver adds to
// "docker hosts create"
func (d *Driver) GetCreateFlags() []mcnflag.Flag {
	return []mcnflag.Flag{
		mcnflag.StringFlag{
			Name:   "opennebula-memory",
			Usage:  "Size of memory for VM in MB",
			EnvVar: "ONE_MEMORY",
			Value:  defaultMemory,
		},
		mcnflag.StringFlag{
			Name:   "opennebula-cpu",
			Usage:  "CPU value for the VM",
			EnvVar: "ONE_CPU",
			Value:  defaultCPU,
		},
		mcnflag.StringFlag{
			Name:   "opennebula-ssh-user",
			Usage:  "Set the name of the SSH user",
			EnvVar: "ONE_SSH_USER",
			Value:  defaultSSHUser,
		},
		mcnflag.StringFlag{
			Name:   "opennebula-vcpu",
			Usage:  "VCPUs for the VM",
			EnvVar: "ONE_VCPU",
			Value:  defaultCPU,
		},
		mcnflag.StringFlag{
			Name:   "opennebula-disk-size",
			Usage:  "Size of disk for VM in MB",
			EnvVar: "ONE_DISK_SIZE",
			Value:  defaultDiskSize,
		},
		mcnflag.StringFlag{
			Name:   "opennebula-network-name",
			Usage:  "Network to connect the machine to",
			EnvVar: "ONE_NETWORK_NAME",
			Value:  "",
		},
		mcnflag.StringFlag{
			Name:   "opennebula-network-id",
			Usage:  "Network ID to connect the machine to",
			EnvVar: "ONE_NETWORK_ID",
			Value:  "",
		},
		mcnflag.StringFlag{
			Name:   "opennebula-network-owner",
			Usage:  "User ID of the Network to connect the machine to",
			EnvVar: "ONE_NETWORK_OWNER",
			Value:  "",
		},
		mcnflag.StringFlag{
			Name:   "opennebula-datastore-id",
			Usage:  "Datastore ID of the Boot2Docker image",
			EnvVar: "ONE_DATASTORE_ID",
			Value:  defaultDatastoreId,
		},
		mcnflag.StringFlag{
			Name:   "opennebula-boot2docker-url",
			Usage:  "The URL of the boot2docker image. By default it uses one hosted by OpenNebula.org",
			EnvVar: "ONE_BOOT2DOCKER_URL",
			Value:  defaultBoot2DockerURL,
		},
	}
}

func (d *Driver) SetConfigFromFlags(flags drivers.DriverOptions) error {
	d.CPU = flags.String("opennebula-cpu")
	d.VCPU = flags.String("opennebula-vcpu")
	d.Memory = flags.String("opennebula-memory")
	d.DiskSize = flags.String("opennebula-disk-size")
	d.NetworkName = flags.String("opennebula-network-name")
	d.NetworkId = flags.String("opennebula-network-id")
	d.NetworkOwner = flags.String("opennebula-network-owner")
	d.DatastoreId = flags.String("opennebula-datastore-id")
	d.Boot2DockerURL = flags.String("opennebula-boot2docker-url")
	d.SSHUser = flags.String("opennebula-ssh-user")

	if d.NetworkName == "" && d.NetworkId == "" {
		return errors.New("Please specify a network to connect to with --opennebula-network-name or --opennebula-network-id.")
	}

 	if d.NetworkName != "" && d.NetworkId != "" {
		return errors.New("Please specify a network to connect to either with  --opennebula-network-name or --opennebula-network-id, not both.")
	}
	return nil
}

func (d *Driver) DriverName() string {
	return "opennebula"
}

func (d *Driver) GetSSHHostname() (string, error) {
	return d.GetIP()
}

func (d *Driver) GetSSHUsername() string {
	return d.SSHUser
}

func (d *Driver) PreCreateCheck() error {
	return nil
}

func (d *Driver) Create() error {
	var (
		err       error
		b2d_id    uint
		ds_id     uint64
		b2d_image *goca.Image
	)

	// Import Boot2Docker
	b2d_name := fmt.Sprintf("b2d-%s", d.MachineName)

	b2d_image, err = goca.NewImageFromName(b2d_name)
	if err != nil {
		b2d_template := goca.NewTemplateBuilder()
		b2d_template.AddValue("name", b2d_name)
		b2d_template.AddValue("path", d.Boot2DockerURL)

		ds_id, err = strconv.ParseUint(d.DatastoreId, 10, 32)
		if err != nil {
			return err
		}

		b2d_id, err = goca.CreateImage(b2d_template.String(), uint(ds_id))
		if err != nil {
			return err
		}

		b2d_image = goca.NewImage(b2d_id)

		b2d_state := ""
		for b2d_state != "READY" {
			err = b2d_image.Info()
			if err != nil {
				return err
			}

			b2d_state, err = b2d_image.StateString()
			if err != nil {
				return err
			}

			switch b2d_state {
			case "INIT", "LOCKED":
				time.Sleep(1 * time.Second)
			case "READY":
			default:
				log.Errorf("Unexpected image state %s", b2d_state)
				return errors.New("Unexpected image state")
			}
		}

		log.Infof("Boot2Docker image registered...")
	} else {
		b2d_id = b2d_image.Id
	}

	log.Infof("Creating SSH key...")
	if err := ssh.GenerateSSHKey(d.GetSSHKeyPath()); err != nil {
		return err
	}

	pubKey, err := ioutil.ReadFile(d.publicSSHKeyPath())
	if err != nil {
		return err
	}

	// Create template
	template := goca.NewTemplateBuilder()
	template.AddValue("NAME", d.MachineName)
	template.AddValue("CPU", d.CPU)
	template.AddValue("MEMORY", d.Memory)

	if d.VCPU != "" {
		template.AddValue("VCPU", d.VCPU)
	}

	vector := template.NewVector("NIC")
	if d.NetworkName != "" {
		vector.AddValue("NETWORK", d.NetworkName)
  	 	if d.NetworkOwner != "" {
			vector.AddValue("NETWORK_UNAME", d.NetworkOwner)
		}
	}
        if d.NetworkId != "" {
		vector.AddValue("NETWORK_ID", d.NetworkId)
	}
	vector = template.NewVector("DISK")
	vector.AddValue("IMAGE_ID", b2d_id)
	vector.AddValue("DEV_PREFIX", "sd")

	vector = template.NewVector("DISK")
	vector.AddValue("FORMAT", "raw")
	vector.AddValue("TYPE", "fs")
	vector.AddValue("SIZE", string(d.DiskSize))
	vector.AddValue("DEV_PREFIX", "sd")

	vector = template.NewVector("CONTEXT")
	vector.AddValue("NETWORK", "YES")
	vector.AddValue("SSH_PUBLIC_KEY", string(pubKey))

	vector = template.NewVector("GRAPHICS")
	vector.AddValue("LISTEN", "0.0.0.0")
	vector.AddValue("TYPE", "vnc")

	// Instantiate
	log.Infof("Starting  VM...")
	_, err = goca.CreateVM(template.String(), false)
	if err != nil {
		return err
	}

	if d.IPAddress, err = d.GetIP(); err != nil {
		return err
	}

	if err := d.Start(); err != nil {
		return err
	}

	return nil
}

func (d *Driver) GetURL() (string, error) {
	ip, err := d.GetIP()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("tcp://%s:2376", ip), nil
}

func (d *Driver) GetIP() (string, error) {
	vm, err := goca.NewVMFromName(d.MachineName)
	if err != nil {
		return "", err
	}

	err = vm.Info()
	if err != nil {
		return "", err
	}

	if ip, ok := vm.XPath("/VM/TEMPLATE/NIC/IP"); ok {
		d.IPAddress = ip
	}

	if d.IPAddress == "" {
		return "", fmt.Errorf("IP address is not set")
	}

	return d.IPAddress, nil
}

func (d *Driver) GetState() (state.State, error) {
	vm, err := goca.NewVMFromName(d.MachineName)
	if err != nil {
		return state.None, err
	}

	err = vm.Info()
	if err != nil {
		return state.None, err
	}

	vm_state, lcm_state, err := vm.StateString()
	if err != nil {
		return state.None, err
	}

	switch vm_state {
	case "INIT", "PENDING", "HOLD":
		return state.Starting, nil
	case "ACTIVE":
		switch lcm_state {
		case "RUNNING",
			"DISK_SNAPSHOT",
			"DISK_SNAPSHOT_REVERT",
			"DISK_SNAPSHOT_DELETE",
			"HOTPLUG",
			"HOTPLUG_SNAPSHOT",
			"HOTPLUG_NIC",
			"HOTPLUG_SAVEAS":
			return state.Running, nil
		case "PROLOG",
			"BOOT",
			"MIGRATE",
			"PROLOG_MIGRATE",
			"PROLOG_RESUME",
			"CLEANUP_RESUBMIT",
			"BOOT_UNKNOWN",
			"BOOT_POWEROFF",
			"BOOT_SUSPENDED",
			"BOOT_STOPPED",
			"PROLOG_UNDEPLOY",
			"BOOT_UNDEPLOY",
			"BOOT_MIGRATE",
			"PROLOG_MIGRATE_SUSPEND",
			"SAVE_MIGRATE":
			return state.Starting, nil
		case "HOTPLUG_SAVEAS_POWEROFF",
			"DISK_SNAPSHOT_POWEROFF",
			"DISK_SNAPSHOT_REVERT_POWEROFF",
			"DISK_SNAPSHOT_DELETE_POWEROFF",
			"HOTPLUG_PROLOG_POWEROFF",
			"HOTPLUG_EPILOG_POWEROFF",
			"PROLOG_MIGRATE_POWEROFF",
			"SAVE_STOP":
			return state.Stopped, nil
		case "HOTPLUG_SAVEAS_SUSPENDED",
			"DISK_SNAPSHOT_SUSPENDED",
			"DISK_SNAPSHOT_REVERT_SUSPENDED",
			"DISK_SNAPSHOT_DELETE_SUSPENDED":
			return state.Saved, nil
		case "EPILOG_STOP",
			"EPILOG",
			"SHUTDOWN_UNDEPLOY",
			"EPILOG_UNDEPLOY",
			"SAVE_SUSPEND",
			"SHUTDOWN",
			"SHUTDOWN_POWEROFF",
			"CANCEL",
			"CLEANUP_DELETE":
			return state.Stopping, nil
		case "UNKNOWN",
			"FAILURE",
			"BOOT_FAILURE",
			"BOOT_MIGRATE_FAILURE",
			"PROLOG_MIGRATE_FAILURE",
			"PROLOG_FAILURE",
			"EPILOG_FAILURE",
			"EPILOG_STOP_FAILURE",
			"EPILOG_UNDEPLOY_FAILURE",
			"PROLOG_MIGRATE_POWEROFF_FAILURE",
			"PROLOG_MIGRATE_SUSPEND_FAILURE",
			"BOOT_UNDEPLOY_FAILURE",
			"BOOT_STOPPED_FAILURE",
			"PROLOG_RESUME_FAILURE",
			"PROLOG_UNDEPLOY_FAILURE":
			return state.Error, nil
		}
	case "POWEROFF", "UNDEPLOYED":
		return state.Stopped, nil
	case "STOPPED", "SUSPENDED":
		return state.Saved, nil
	case "DONE", "FAILED":
		return state.Error, nil
	}

	return state.Error, nil
}

func (d *Driver) Start() error {
	vm, err := goca.NewVMFromName(d.MachineName)
	if err != nil {
		return err
	}

	vm.Resume()

	s := state.None
	for retry := 0; retry < 50 && s != state.Running; retry++ {
		s, err = d.GetState()
		if err != nil {
			return err
		}

		switch s {
		case state.Running:
		case state.Error:
			return errors.New("VM in error state")
		default:
			time.Sleep(2 * time.Second)
		}
	}

	if d.IPAddress == "" {
		if d.IPAddress, err = d.GetIP(); err != nil {
			return err
		}
	}

	log.Infof("Waiting for SSH...")
	// Wait for SSH over NAT to be available before returning to user
	if err := drivers.WaitForSSH(d); err != nil {
		return err
	}

	return nil
}

func (d *Driver) Stop() error {
	vm, err := goca.NewVMFromName(d.MachineName)
	if err != nil {
		return err
	}

	err = vm.PowerOff()
	if err != nil {
		return err
	}

	return nil
}

func (d *Driver) Remove() error {
	vm, err := goca.NewVMFromName(d.MachineName)
	if err != nil {
		return err
	}

	err = vm.ShutdownHard()
	if err != nil {
		return err
	}

	return nil
}

func (d *Driver) Restart() error {
	vm, err := goca.NewVMFromName(d.MachineName)
	if err != nil {
		return err
	}

	err = vm.Reboot()
	if err != nil {
		return err
	}

	return nil
}

func (d *Driver) Kill() error {
	vm, err := goca.NewVMFromName(d.MachineName)
	if err != nil {
		return err
	}

	err = vm.PowerOffHard()
	if err != nil {
		return err
	}

	return nil
}

func (d *Driver) publicSSHKeyPath() string {
	return d.GetSSHKeyPath() + ".pub"
}
