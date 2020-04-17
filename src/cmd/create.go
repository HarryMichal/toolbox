/*
Copyright © 2019 Red Hat Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"github.com/briandowns/spinner"

	"github.com/containers/toolbox/pkg/podman"
	"github.com/containers/toolbox/pkg/utils"

	"github.com/godbus/dbus/v5"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	createFlags struct {
		image   string
		release string
		unsafe  bool
	}
	ulimitHost               = []string{}
	homeCanonical            = ""
	homeLink                 = ""
	mediaLink                = ""
	mntLink                  = ""
	toolboxProfileBind       = []string{}
	sudoGroup                = ""
	kcmSocket                = ""
	kcmSocketBind            = []string{}
	usrMountPoint            = ""
	usrMountSourceFlags      = ""
	usrMountDestinationFlags = "ro"
	dbusSystemBusAddress     = ""
	preservedEnvVars         = []string{
		"COLORTERM",
		"COLUMNS",
		"DBUS_SESSION_BUS_ADDRESS",
		"DBUS_SYSTEM_BUS_ADDRESS",
		"DESKTOP_SESSION",
		"DISPLAY",
		"LANG",
		"LINES",
		"SSH_AUTH_SOCK",
		"TERM",
		"USER",
		"VTE_VERSION",
		"WAYLAND_DISPLAY",
		"XDG_CURRENT_DESKTOP",
		"XDG_DATA_DIRS",
		"XDG_MENU_PREFIX",
		"XDG_RUNTIME_DIR",
		"XDG_SEAT",
		"XDG_SESSION_DESKTOP",
		"XDG_SESSION_ID",
		"XDG_SESSION_TYPE",
		"XDG_VTNR",
	}
)

var createCmd = &cobra.Command{
	Use:   "create [flags] NAME",
	Short: "Create a new toolbox container",
	RunE: func(cmd *cobra.Command, args []string) error {
		return create(args)
	},
	Args: cobra.MaximumNArgs(1),
}

func init() {
	rootCmd.AddCommand(createCmd)

	flags := createCmd.Flags()
	flags.StringVarP(&createFlags.image, "image", "i", "", "Change the name of the base image used to create the toolbox container")
	flags.StringVarP(&createFlags.release, "release", "r", "", "Create a toolbox container for a different operating system release than the host")
	flags.BoolVar(&createFlags.unsafe, "unsafe", false, "Allow the use of a non-Toolbox-compliant image")

	viper.SetDefault("DBUS_SYSTEM_BUS_ADDRESS", "unix:path=/var/run/dbus/system_bus_socket")
}

func create(args []string) error {
	containerName := ""
	imageName := ""
	imageDomain := ""

	if len(args) != 0 {
		containerName = args[0]

		if !utils.IsContainerNameValid(containerName) {
			logrus.Fatal("Container names must match ^[a-zA-Z0-9][a-zA-Z0-9_.-]*$")
		}
	}

	// Toolbox should work even when some options are not specified. This is where the default values are defined and existing standardized.
	containerName, imageName, imageDomain = utils.UpdateContainerAndImageNames(containerName, createFlags.image, createFlags.release)

	// Currently Toolbox only trusts one registry: registry.fedoraproject.org
	// But if the provided image name already has a domain then Toolbox will use it
	fullImageName := ""
	if imageDomain == "" {
		fullImageName = fmt.Sprintf("registry.fedoraproject.org/%s", imageName)
	} else {
		fullImageName = fmt.Sprintf("%s/%s", imageDomain, imageName)
	}

	logrus.Infof("Checking if container %s already exists", containerName)
	if podman.ContainerExists(containerName) {
		logrus.Fatalf("Container %s already exists", containerName)
	}

	logrus.Infof("Used image will be: %s", fullImageName)

	// Look for the toolbox image on local machine
	imageFound := findLocalToolboxImage(imageName)

	imagePulled := false
	if !imageFound {
		logrus.Infof("Image '%s' was not found on the host machine", imageName)
		imagePulled, err := pullToolboxImage(fullImageName)
		if !imagePulled {
			if err != nil {
				logrus.Fatalf("Failed to pull image: %v", err)
			}
			// If the image was not pulled but there is no error then the user said no to pulling an image
			os.Exit(0)
		}
		logrus.Infof("Image '%s was pulled", fullImageName)
	} else {
		logrus.Infof("Image '%s' was found", fullImageName)
	}

	// Check if the image is a Toolbox image
	// FIXME: In the future this check will have to be done only for local images because for pulled images it can be done by inspecting their manifest before pulling them
	if imageFound || imagePulled {
		if !createFlags.unsafe {
			isToolboxImage, err := checkIfToolboxImage(imageName)
			if !isToolboxImage {
				logrus.Fatalf("Image '%s' is not a Toolbox image: %v", imageName, err)
			}
			logrus.Infof("Image '%s' is a Toolbox image", imageName)
		} else {
			logrus.Infof("Skipping check if image '%s' is a Toolbox image", imageName)
		}
	}

	logrus.Info("Looking for group for sudo")
	sudoGroup = utils.GetGroupForSudo()
	if sudoGroup == "" {
		logrus.Fatal("Group for sudo was not found")
	}
	logrus.Infof("Group for sudo is %s", sudoGroup)

	logrus.Info("Getting user ID")
	currentUser, err := user.Current()
	if err != nil {
		logrus.Fatal("Failed to get user information")
	}
	userID := currentUser.Uid
	logrus.Infof("User ID is %s", userID)

	// Start assembling the arguments for Podman
	createArgs := []string{
		"create",
		"--dns", "none",
		"--env", fmt.Sprintf("TOOLBOX_PATH=%s", viper.GetString("TOOLBOX_PATH")),
		"--group-add", sudoGroup,
		"--hostname", "toolbox",
		"--ipc", "host",
		"--label", "com.github.containers.toolbox=true",
		"--name", containerName,
		"--network", "host",
		"--no-hosts",
		"--pid", "host",
		"--privileged",
		"--security-opt", "label=disable",
		"--userns=keep-id",
		"--user", "root:root"}

	command := []string{
		"toolbox", "--log-level", "debug", "init-container",
		"--home", viper.GetString("HOME"),
		"--monitor-host",
		"--shell", viper.GetString("SHELL"),
		"--uid", userID,
		"--user", viper.GetString("USER")}

	logrus.Info("Checking if toolbox.sh profile exists")
	if utils.PathExists("/etc/profile.d/toolbox.sh") {
		logrus.Info("Found /etc/profile.d/toolbox.sh")

		toolboxProfileBind = []string{"--volume", "/etc/profile.d/toolbox.sh:/etc/profile.d/toolbox.sh:ro"}
		createArgs = append(createArgs, toolboxProfileBind...)
	} else if utils.PathExists("/usr/share/profile.d/toolbox.sh") {
		logrus.Info("Found /usr/share/profile.d/toolbox.sh")

		toolboxProfileBind = []string{"--volume", "/usr/share/profile.d/toolbox.sh:/etc/profile.d/toolbox.sh:ro"}
	} else {
		logrus.Info("File 'toolbox.sh' does not exist in any known location")
	}

	if utils.PathExists("/media") {
		logrus.Info("Checking if /media is a symbolic link to /run/media")

		mediaPath, err := filepath.EvalSymlinks("/media")
		if err != nil {
			logrus.Error(err)
		}

		if mediaPath == "run/media" {
			logrus.Info("/media is a symbolic link to /run/media")
			command = append(command, "--media-link")
		} else {
			mediaBind := []string{"--volume", "/media:/media:rslave"}
			createArgs = append(createArgs, mediaBind...)
		}
	}

	logrus.Info("Checking if /mnt is a symbolic link to /var/mnt")

	mntPath, err := filepath.EvalSymlinks("/mnt")
	if err != nil {
		logrus.Error(err)
	}

	if mntPath == "var/mnt" {
		logrus.Info("/mnt is a symbolic link to /var/mnt")
		command = append(command, "--mnt-link")
	} else {
		mntBind := []string{"--volume", "/mnt:/mnt:rslave"}
		createArgs = append(createArgs, mntBind...)
	}

	if utils.PathExists("/run/media") {
		runMediaBind := []string{"--volume", "/run/media:/run/media:rslave"}
		createArgs = append(createArgs, runMediaBind...)
	}

	logrus.Info("Checking if /usr is mounted read-only or read-write")
	usrMountPoint, err := utils.GetMountPoint("/usr")
	if err != nil {
		logrus.Error(err)
		logrus.Fatal("Failed to get the mount-point of /usr")
	}
	logrus.Infof("Mount-point of /usr is %s", usrMountPoint)
	usrMountSourceFlags, err = utils.GetMountOptions(usrMountPoint)
	if err != nil {
		logrus.Error(err)
		logrus.Fatalf("Failed to get the mount options of %s", usrMountPoint)
	}
	logrus.Infof("Mount flags of /usr on the host are %s", usrMountSourceFlags)

	if !strings.Contains(usrMountSourceFlags, "ro") {
		usrMountDestinationFlags = "rw"
	}

	// Inside of a toolbox we want to be able to access dbus for using flatpak-spawn and for users, who work with dbus.
	logrus.Info("Preparing dbus system bus address")
	dbusSystemBusAddress = strings.Split(viper.GetString("DBUS_SYSTEM_BUS_ADDRESS"), "=")[1]
	dbusSystemBusAddress, err = filepath.EvalSymlinks(dbusSystemBusAddress)
	if err != nil {
		logrus.Error(err)
	}

	dbusSystemBusAddressBind := []string{"--volume", fmt.Sprintf("%s:%s", dbusSystemBusAddress, dbusSystemBusAddress)}
	createArgs = append(createArgs, dbusSystemBusAddressBind...)

	logrus.Info("Preparing sssd-kcm socket")
	args = []string{"show", "--value", "--property", "Listen", "sssd-kcm.socket"}
	output, err := utils.SystemctlOutput(args...)
	if err != nil {
		logrus.Error("Failed to use 'systemctl show'")
	}

	kcmSocket = strings.Trim(string(output), "\n")

	if kcmSocket == "" {
		logrus.Error("Failed to read property Listen from sssd-kcm.socket")
	} else {
		logrus.Infof("Checking value %s of property Listen in sssd-kcm.socket", kcmSocket)
		if !strings.Contains(kcmSocket, " (Stream)") {
			kcmSocket = ""
			logrus.Error("Unknown socket in sssd-kcm.socket\nExpected SOCK_STREAM")
		}
		if !strings.Contains(kcmSocket, "/") {
			kcmSocket = ""
			logrus.Error("Unknown socket in sssd-kcm.socket\nExpected file system socket in the AF_UNIX family")
		}
	}

	logrus.Infof("Parsing value %s of property Listen in sssd-kcm.socket", kcmSocket)
	if kcmSocket != "" {
		kcmSocket = strings.TrimSuffix(kcmSocket, " (Stream)")
		kcmSocketBind = []string{"--volume", fmt.Sprintf("%s:%s", kcmSocket, kcmSocket)}
		createArgs = append(createArgs, kcmSocketBind...)
	}

	logrus.Info("Checking if 'podman create' supports option '--ulimit host'")
	if podman.CheckVersion("1.5.0") {
		logrus.Info("Option '--ulimit host' is supported")
		ulimitHost = []string{"--ulimit", "host"}
	} else {
		logrus.Info("Option '--ulimit host' is not supported")
	}

	homeEnv := strings.Split(viper.GetString("HOME"), "=")[0]
	homeCanonical, err = filepath.EvalSymlinks(homeEnv)
	if err != nil {
		logrus.Fatalf("Failed to canonicalize %s", homeEnv)
	}
	logrus.Infof("Canonicalized %s to %s", homeEnv, homeCanonical)

	logrus.Info("Checking if /home is a symbolic link to /var/home")
	homeSymPath, err := filepath.EvalSymlinks("/home")
	if err != nil {
		logrus.Error("Failed to evaluate if /home is a symbolic link")
	}
	if homeSymPath == "/var/home" {
		logrus.Info("/home is a symbolic link to /var/home")
		command = append(command, "--home-link")
	}

	conn, err := dbus.SessionBus()
	if err != nil {
		logrus.Error("Failed to connect to Session Bus")
	}
	defer conn.Close()

	logrus.Info("Calling org.freedesktop.Flatpak.SessionHelper.RequestSession")
	SessionHelper := conn.Object("org.freedesktop.Flatpak", "/org/freedesktop/Flatpak/SessionHelper")
	call := SessionHelper.Call("org.freedesktop.Flatpak.SessionHelper.RequestSession", 0)
	if call.Err != nil {
		logrus.Fatal("Failed to call org.freedesktop.Flatpak.SessionHelper.RequestSession")
	}

	// Add the environment variables that hold a value
	for _, env := range preservedEnvVars {
		value := viper.GetString(env)
		if len(value) != 0 {
			createArgs = append(createArgs, fmt.Sprintf("--env=%s=%s", env, value))
		}
	}

	createArgs = append(createArgs, []string{
		"--volume", fmt.Sprintf("%s:/usr/bin/toolbox:ro", viper.GetString("TOOLBOX_PATH")),
		"--volume", fmt.Sprintf("%s:%s", viper.GetString("XDG_RUNTIME_DIR"), viper.GetString("XDG_RUNTIME_DIR")),
		"--volume", fmt.Sprintf("%s/.flatpak-helper/monitor:/run/host/monitor", viper.GetString("XDG_RUNTIME_DIR")),
		"--volume", fmt.Sprintf("%s:%s:rslave", homeCanonical, homeCanonical),
		"--volume", "/etc:/run/host/etc",
		"--volume", "/dev:/dev:rslave",
		"--volume", "/run:/run/host/run:rslave",
		"--volume", "/tmp:/run/host/tmp:rslave",
		"--volume", fmt.Sprintf("/usr:/run/host/usr:%s,rslave", usrMountDestinationFlags),
		"--volume", "/var:/run/host/var:rslave",
		imageName}...)

	createArgs = append(createArgs, command...)

	logrus.Infof("Trying to create container %s", containerName)
	logrus.Debug(createArgs)

	output, err = podman.CmdOutput(createArgs...)
	if err != nil {
		logrus.Debug(err)
		logrus.Fatalf("Failed to create container %s", containerName)
	}

	fmt.Printf("Created container: %s\n", containerName)
	fmt.Printf("Enter with: toolbox enter %s\n", containerName)

	return nil
}

func findLocalToolboxImage(imageName string) bool {
	logrus.Info("Looking for the image locally")

	logrus.Infof("Looking for image %s", imageName)
	if podman.ImageExists(imageName) {
		return true
	}

	hasDomain := utils.ReferenceHasDomain(imageName)

	if !hasDomain {
		imageName = "localhost/" + imageName
		logrus.Infof("Looking for image %s", imageName)

		if podman.ImageExists(imageName) {
			return true
		}
	}

	return false
}

func checkIfToolboxImage(imageName string) (bool, error) {
	logrus.Infof("Checking if '%s' is a Toolbox image", imageName)
	inspectInfo, err := podman.PodmanInspect("image", imageName)
	if err != nil {
		return false, errors.New("Unable to inspect image")
	}
	var imageLabels map[string]interface{}
	if inspectInfo["Labels"] == nil {
		return false, errors.New("Image does not have labels at all")
	}

	imageLabels = inspectInfo["Labels"].(map[string]interface{})
	isToolboxImage := false
	if imageLabels["com.github.debarshiray.toolbox"] == "true" {
		isToolboxImage = true
	}
	if imageLabels["com.github.containers.toolbox"] == "true" {
		isToolboxImage = true
	}

	if !isToolboxImage {
		return false, errors.New("Image does not have the right labels")
	}

	return true, nil
}

func pullToolboxImage(fullImageName string) (bool, error) {
	pullImage := false
	imagePulled := false

	if !rootFlags.assumeyes {
		response := ""
		fmt.Println("Image required to create toolbox container.")
		fmt.Printf("Do you want to pull %s (+-200MB)? [y/N]: ", fullImageName)
		fmt.Scanf("%s", &response)
		response = strings.ToLower(response)
		if response == "y" || response == "yes" {
			pullImage = true
		}
	} else {
		pullImage = true
	}

	if pullImage {
		s := spinner.New(spinner.CharSets[9], 500*time.Millisecond)
		s.Prefix = fmt.Sprintf("Pulling %s ", fullImageName)
		s.Writer = os.Stderr
		s.Start()
		defer s.Stop()

		retries := 0
		for ; retries < 4; retries++ {
			err := podman.PullImage(fullImageName)
			if err != nil {
				if errors.Is(err, podman.ErrServiceUnavailable) {
					logrus.Debug("Received Service Unavailable error. Trying again to pull the image.")
					time.Sleep(200 * time.Millisecond)
					continue
				}
				return false, err
			}
			break
		}

		if retries == 3 && !imagePulled {
			return false, errors.New("Received Service Unavailable 3 times")
		}

		return true, nil
	}

	return false, nil
}