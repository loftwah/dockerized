package main

import (
	"context"
	"fmt"
	"github.com/compose-spec/compose-go/cli"
	"github.com/compose-spec/compose-go/types"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/flags"
	"github.com/docker/compose/v2/pkg/api"
	"github.com/docker/compose/v2/pkg/compose"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"syscall"
)

var options = []string{
	"--shell",
	"--build",
	"-h",
	"--help",
	"-v",
	"--verbose",
}

var dockerizedEnvFileName = "dockerized.env"

func getDockerizedRoot() string {
	return filepath.Dir(filepath.Dir(os.Args[0]))
}

func contains(s []string, str string) bool {
	for _, v := range s {
		if v == str {
			return true
		}
	}

	return false
}

func findLocalEnvFile(path string) (string, error) {
	envFilePath := ""
	for i := 0; i < 10; i++ {
		envFilePath = filepath.Join(path, dockerizedEnvFileName)
		if _, err := os.Stat(envFilePath); err == nil {
			return envFilePath, nil
		}
		path = filepath.Dir(path)
	}
	return "", fmt.Errorf("no local %s found", dockerizedEnvFileName)
}

func normalizeEnvironment() {
	homeDir, _ := os.UserHomeDir()
	if os.Getenv("HOME") == "" {
		os.Setenv("HOME", homeDir)
	}
}
func newSigContext() (context.Context, func()) {
	ctx, cancel := context.WithCancel(context.Background())
	s := make(chan os.Signal, 1)
	signal.Notify(s, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-s
		cancel()
	}()
	return ctx, cancel
}

func getProject(dockerComposeFilePath string) (*types.Project, error) {
	options, err := cli.NewProjectOptions([]string{
		dockerComposeFilePath,
	},
		cli.WithDotEnv,
		cli.WithOsEnv,
		cli.WithConfigFileEnv,
	) //, cli.WithDefaultConfigPath

	if err != nil {
		return nil, nil
	}

	return cli.ProjectFromOptions(options)
}

func test(dockerComposeFilePath string) {
	//apiCli, err := client.NewClientWithOpts(client.FromEnv)
	//if err != nil {
	//	panic(err)
	//}
	dockerCli, err := command.NewDockerCli()
	dockerCliOpts := flags.NewClientOptions()
	err = dockerCli.Initialize(dockerCliOpts)
	if err != nil {
		return
	}

	if err != nil {
		panic(err)
	}

	// call compose
	var lazyInit = api.NewServiceProxy()
	lazyInit.WithService(compose.NewComposeService(dockerCli))

	// get from env COMPOSE_FILE

	project, err := getProject(dockerComposeFilePath)
	if err != nil {
		panic(err)
	}

	ctx, _ := newSigContext()

	serviceName := "go"

	// find service with name "go"
	service, err := project.GetService(serviceName)
	service.CustomLabels = map[string]string{}
	project.Services = []types.ServiceConfig{service}

	opts := api.RunOptions{
		//Name:              "",
		Service: serviceName,
		Command: []string{"version"},
		//Entrypoint:        nil,
		Detach:     false,
		AutoRemove: true,
		Tty:        true,
		//WorkingDir:        "",
		//User:              "",
		//Environment:       nil,
		//Labels:            nil,
		//Privileged:        false,
		//UseNetworkAliases: false,
		//NoDeps:            false,
		//QuietPull:         false,
		//Index:             0,
	}

	createOptions := api.CreateOptions{
		Services:      []string{serviceName},
		RemoveOrphans: true,
	}
	lazyInit.Create(ctx, project, createOptions)
	exitCode, err := lazyInit.RunOneOffContainer(ctx, project, opts)
	if err != nil {
		fmt.Println(err)
		return
	}
	if exitCode != 0 {
		panic(fmt.Errorf("docker-compose exited with code %d", exitCode))
	}

	//containers, err := cli.ContainerList(context.Background(), types.ContainerListOptions{})
	//if err != nil {
	//	panic(err)
	//}
	//
	//for _, container := range containers {
	//	fmt.Printf("%s %s\n", container.ID[:10], container.Image)
	//}
}

func main() {

	normalizeEnvironment()

	command := ""
	var commandArgs []string
	var dockerizedOptions []string
	for _, arg := range os.Args[1:] {
		if arg[0] == '-' && command == "" {
			if contains(options, arg) {
				dockerizedOptions = append(dockerizedOptions, arg)
			} else {
				fmt.Println("Unknown option:", arg)
				os.Exit(1)
			}
		} else {
			if command == "" {
				command = arg
			} else {
				commandArgs = append(commandArgs, arg)
			}
		}
	}

	dockerizedRoot := getDockerizedRoot()
	dockerizedDockerComposeFilePath := filepath.Join(dockerizedRoot, "docker-compose.yml")

	dockerizedDockerComposeFilePath = os.Getenv("COMPOSE_FILE")
	test(dockerizedDockerComposeFilePath)
	os.Exit(1)
	var optionHelp = contains(dockerizedOptions, "--help") || contains(dockerizedOptions, "-h")
	var optionVerbose = contains(dockerizedOptions, "--verbose") || contains(dockerizedOptions, "-v")

	if command == "" || optionHelp {
		help(dockerizedDockerComposeFilePath)
		if optionHelp {
			os.Exit(0)
		} else {
			os.Exit(1)
		}
	}

	hostCwd, _ := os.Getwd()
	hostCwdDirName := filepath.Base(hostCwd)
	composeRunArgs := []string{
		"-f", dockerizedDockerComposeFilePath,
		"run", "--rm",
		"-v", hostCwd + ":" + "/host/" + hostCwdDirName,
		"-w", "/host/" + hostCwdDirName,
	}

	hostName, _ := os.Hostname()
	composeRunArgs = append(composeRunArgs, "-e", "HOST_HOSTNAME="+hostName)

	if contains(dockerizedOptions, "--shell") {
		composeRunArgs = append(composeRunArgs, "--entrypoint=sh")
		commandArgs = []string{
			"-c",
			"$(which bash sh zsh | head -n 1)",
		}
	} else if contains(dockerizedOptions, "--build") {
		err := dockerCompose([]string{
			"-f", dockerizedDockerComposeFilePath,
			"build",
			command,
		})
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	}
	composeRunArgs = append(composeRunArgs, command)
	composeRunArgs = append(composeRunArgs, commandArgs...)

	// fmt.Printf("composeRunArgs: %v\n", composeRunArgs)

	homeDir, _ := os.UserHomeDir()
	userGlobalDockerizedEnvFile := filepath.Join(homeDir, dockerizedEnvFileName)
	localDockerizedEnvFile, err := findLocalEnvFile(hostCwd)

	var envFiles []string

	if _, err := os.Stat(userGlobalDockerizedEnvFile); err == nil {
		envFiles = append(envFiles, userGlobalDockerizedEnvFile)
	}
	if err == nil && !contains(envFiles, localDockerizedEnvFile) {
		envFiles = append(envFiles, localDockerizedEnvFile)
	}

	if optionVerbose {
		// Print it in order of priority (lowest to highest)
		for _, envFile := range envFiles {
			fmt.Println("Loading: ", envFile)
		}
	}
	// Load in reverse. GoDotEnv does not override vars, this allows runtime env-vars to override the env files.
	for i := len(envFiles) - 1; i >= 0; i-- {
		//godotenv.Load(envFiles[i])
	}

	err = dockerCompose(composeRunArgs)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func help(dockerComposeFilePath string) error {
	fmt.Println("Usage: dockerized [options] <command> [args]")
	fmt.Println("")
	fmt.Println("Commands:")

	services, err := getServices(dockerComposeFilePath)
	if err != nil {
		return err
	}

	sort.Strings(services)
	for _, service := range services {
		if service[0] == '_' {
			continue
		}
		fmt.Printf("  %s\n", service)
	}

	fmt.Println("")

	fmt.Println("Options:")
	fmt.Println("  --help, -h Show this help")
	fmt.Println("  --shell    Start a shell inside the command container. Similar to `docker run --entrypoint=sh`.")
	fmt.Println("  --build    Rebuild the container before running it.")

	fmt.Println()
	fmt.Println("Args:")
	fmt.Println("  <command> [args]  Arguments are passed to the command within the container.")

	return nil
}

func getServices(dockerComposeFilePath string) ([]string, error) {
	//dockerComposeFileBytes, err := ioutil.ReadFile(dockerComposeFilePath)
	//if err != nil {
	//	return nil, err
	//}
	//config, err := loader.ParseYAML(dockerComposeFileBytes)
	//if err != nil {
	//	return nil, err
	//}
	//
	//serviceMaps := config["services"].(map[string]interface{})
	var services []string
	//for service := range serviceMaps {
	//	services = append(services, service)
	//}
	return services, nil
}

func dockerCompose(composeRunArgs []string) error {
	cmd := exec.Command("docker-compose", composeRunArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		return err
	}
	return nil
}
