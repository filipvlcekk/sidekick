/*
Copyright © 2024 Mahmoud Mousa <m.mousa@hey.com>

Licensed under the GNU GPL License, Version 3.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
https://www.gnu.org/licenses/gpl-3.0.en.html

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package deploy

import (
	"crypto/md5"
	"fmt"
	"net"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/log"
	teaLog "github.com/charmbracelet/log"
	"github.com/mightymoud/sidekick/render"
	"github.com/mightymoud/sidekick/utils"
	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/ssh"
	"gopkg.in/yaml.v3"
)

func prelude(config *utils.SidekickConfig) (utils.SidekickAppConfig, utils.SidekickServer) {
	if !utils.FileExists("./sidekick.yml") {
		pterm.Error.Println(`Sidekick config not found in current directory Run sidekick launch`)
		os.Exit(1)
	}

	appConfig, loadError := utils.LoadAppConfig()
	if loadError != nil {
		panic(loadError)
	}

	// Older version of app config does not have server name
	// We will try to match the first server with IP matching the url
	if appConfig.Server == "" {
		render.GetLogger(teaLog.Options{Prefix: "Backward Compat"}).Info("An older version of sidekick.yml is found. Attempting to match a server from url...")
		ips, err := net.LookupIP(appConfig.Url)
		if err != nil {
			render.GetLogger(teaLog.Options{Prefix: "Backward Compat"}).Fatal(err)
		}
		for _, server := range config.Servers {
			if slices.ContainsFunc(ips, func(ip net.IP) bool {
				return ip.String() == server.Address
			}) {
				appConfig.Server = server.Name
				render.GetLogger(teaLog.Options{Prefix: "Backward Compat"}).Info("Found server", "name", server.Name, "IP", server.Address)
				break
			}
		}
	}

	if appConfig.Server == "" {
		render.GetLogger(teaLog.Options{Prefix: "Backward Compat"}).Fatal("Unable to find a server that this app was deployed.")
	}

	server, err := config.FindServer(appConfig.Server)
	if err != nil {
		render.GetLogger(teaLog.Options{Prefix: "Sidekick Config"}).Fatal(err)
	}

	if server.SecretKey == "" {
		render.GetLogger(teaLog.Options{Prefix: "Backward Compat"}).Error("Recent changes to how Sidekick handles secrets prevents you from launcing a new application.")
		render.GetLogger(teaLog.Options{Prefix: "Backward Compat"}).Info("To fix this, run `Sidekick init` with the same server address you have now.")
		render.GetLogger(teaLog.Options{Prefix: "Backward Compat"}).Info("Learn more at www.sidekickdeploy.com/docs/design/encryption")
		os.Exit(1)
	}

	return appConfig, server
}

func stage1Login(server *utils.SidekickServer) (*ssh.Client, error) {
	sshClient, err := utils.Login(server.Address, "sidekick")
	return sshClient, err
}

func stage2EnvFile(appConfig utils.SidekickAppConfig, p *tea.Program, server *utils.SidekickServer) (bool, string, error) {
	defer os.Remove("encrypted.env")
	envFileChanged := false
	currentEnvFileHash := ""
	if appConfig.Env.File != "" {
		envFileContent, envFileErr := os.ReadFile(fmt.Sprintf("./%s", appConfig.Env.File))
		if envFileErr != nil {
			return false, "", fmt.Errorf("failed to read environment file: %w", envFileErr)
		}
		currentEnvFileHash = fmt.Sprintf("%x", md5.Sum(envFileContent))
		envFileChanged = appConfig.Env.Hash != currentEnvFileHash
		if envFileChanged {
			// encrypt new env file
			envCmd := exec.Command("sh", "-s", "-", server.PublicKey, fmt.Sprintf("./%s", appConfig.Env.File))
			envCmd.Stdin = strings.NewReader(utils.EnvEncryptionScript)
			envCmdErrPipe, _ := envCmd.StderrPipe()
			go render.SendLogsToTUI(envCmdErrPipe, p)
			if envCmdErr := envCmd.Run(); envCmdErr != nil {
				return false, "", fmt.Errorf("failed to encrypt environment file: %w", envCmdErr)
			}
			encryptSyncCmd := exec.Command("rsync", "-v", "encrypted.env", fmt.Sprintf("%s@%s:%s", "sidekick", server.Address, fmt.Sprintf("./%s", appConfig.Name)))
			encryptSyncCmdErrPipe, _ := encryptSyncCmd.StderrPipe()
			go render.SendLogsToTUI(encryptSyncCmdErrPipe, p)
			if encryptSyncCmdErr := encryptSyncCmd.Run(); encryptSyncCmdErr != nil {
				return false, "", fmt.Errorf("failed to sync encrypted environment file to server: %w", encryptSyncCmdErr)
			}
		}
	}
	return envFileChanged, currentEnvFileHash, nil
}

func stage3BuildDockerImage(appConfig utils.SidekickAppConfig, p *tea.Program, server *utils.SidekickServer) error {
	cwd, _ := os.Getwd()
	dockerPlatformId := server.PlatformId
	dockerBuildCmd := exec.Command("docker", "build", "--tag", appConfig.Name, "--progress=plain", fmt.Sprintf("--platform=%s", dockerPlatformId), cwd)
	dockerBuildCmdErrPipe, _ := dockerBuildCmd.StderrPipe()
	go render.SendLogsToTUI(dockerBuildCmdErrPipe, p)

	if dockerBuildErr := dockerBuildCmd.Run(); dockerBuildErr != nil {
		return fmt.Errorf("failed to build Docker image: %w", dockerBuildErr)
	}
	return nil
}

func stage4SaveDockerImage(appConfig utils.SidekickAppConfig, p *tea.Program) error {
	imgFileName := fmt.Sprintf("%s-latest.tar", appConfig.Name)
	imgSaveCmd := exec.Command("docker", "save", "-o", imgFileName, appConfig.Name)
	imgSaveCmdErrPipe, _ := imgSaveCmd.StderrPipe()
	go render.SendLogsToTUI(imgSaveCmdErrPipe, p)

	if imgSaveCmdErr := imgSaveCmd.Run(); imgSaveCmdErr != nil {
		return fmt.Errorf("failed to save Docker image: %w", imgSaveCmdErr)
	}
	return nil
}

func stage5MoveDockerImage(appConfig utils.SidekickAppConfig, p *tea.Program, server *utils.SidekickServer) error {
	imgFileName := fmt.Sprintf("%s-latest.tar", appConfig.Name)
	remoteDist := fmt.Sprintf("%s@%s:./%s", "sidekick", server.Address, appConfig.Name)
	imgMoveCmd := exec.Command("scp", "-C", imgFileName, remoteDist)
	imgMoveCmdErrorPipe, _ := imgMoveCmd.StderrPipe()
	go render.SendLogsToTUI(imgMoveCmdErrorPipe, p)

	if imgMovCmdErr := imgMoveCmd.Run(); imgMovCmdErr != nil {
		return fmt.Errorf("failed to move Docker image to server: %w", imgMovCmdErr)
	}
	os.Remove(imgFileName)
	return nil
}

func stage6Deploy(sshClient *ssh.Client, appConfig utils.SidekickAppConfig, envFileChanged bool, currentEnvFileHash string, p *tea.Program, server *utils.SidekickServer) error {
	dockerLoadOutChan, _, sessionErr := utils.RunCommand(sshClient, fmt.Sprintf("cd %s && docker load -i %s-latest.tar", appConfig.Name, appConfig.Name))
	if sessionErr != nil {
		return fmt.Errorf("failed to load docker image on server: %w", sessionErr)
	}

	go func() {
		p.Send(render.LogMsg{LogLine: <-dockerLoadOutChan + "\n"})
		time.Sleep(time.Millisecond * 100)
	}()

	replacer := strings.NewReplacer(
		"$service_name", appConfig.Name,
		"$app_port", fmt.Sprint(appConfig.Port),
		"$has_env", appConfig.Env.File,
	)

	deployScript := replacer.Replace(utils.DeployAppScript)
	utils.RunCommandWithTUIHook(sshClient, deployScript, p, utils.EnvVar{"SOPS_AGE_KEY": server.SecretKey})
	time.Sleep(time.Second * 2)

	cleanOutChan, _, sessionErr := utils.RunCommand(sshClient, fmt.Sprintf("cd %s && rm %s", appConfig.Name, fmt.Sprintf("%s-latest.tar", appConfig.Name)))
	if sessionErr != nil {
		return fmt.Errorf("failed to clean up image file on server: %w", sessionErr)
	}
	go func() {
		p.Send(render.LogMsg{LogLine: <-cleanOutChan + "\n"})
		time.Sleep(time.Millisecond * 100)
	}()

	latestVersion := strings.Split(appConfig.Version, "")[1]
	latestVersionInt, _ := strconv.ParseInt(latestVersion, 0, 64)
	appConfig.Version = fmt.Sprintf("V%d", latestVersionInt+1)
	// env file changed ? -> update hash
	if envFileChanged {
		appConfig.Env.Hash = currentEnvFileHash
	}
	ymlData, _ := yaml.Marshal(&appConfig)
	os.WriteFile("./sidekick.yml", ymlData, 0644)

	return nil
}

var DeployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Deploy a new version of your application to your VPS using Sidekick",
	Long: `This command deploys a new version of your application to your VPS.
It assumes that your VPS is already configured and that your application is ready for deployment`,
	Run: func(cmd *cobra.Command, args []string) {
		start := time.Now()

		config, err := utils.GetSidekickConfigFromCmdContext(cmd)
		if err != nil {
			render.GetLogger(log.Options{Prefix: "Sidekick Config"}).Fatalf("%s", err)
		}
		appConfig, sidekickServer := prelude(config)

		cmdStages := []render.Stage{
			render.MakeStage("Validating connection with VPS", "VPS is reachable", false),
			render.MakeStage("Updating secrets if needed", "Env file check complete", false),
			render.MakeStage("Building latest docker image of your app", "Latest docker image built", true),
			render.MakeStage("Saving docker image locally", "Image saved successfully", false),
			render.MakeStage("Moving image to your server", "Image moved and loaded successfully", false),
			render.MakeStage("Deploying a new version of your application", "Deployed new version successfully", true),
		}
		p := tea.NewProgram(render.TuiModel{
			Stages:      cmdStages,
			BannerMsg:   fmt.Sprintf("Deploying a new env of your app to server %s (%s) 😎", sidekickServer.Name, sidekickServer.Address),
			ActiveIndex: 0,
			Quitting:    false,
			AllDone:     false,
		})

		go func() {
			sshClient, err := stage1Login(&sidekickServer)
			if err != nil {
				p.Send(render.ErrorMsg{ErrorStr: "Failed to connect to VPS: " + err.Error()})
				return
			}
			p.Send(render.NextStageMsg{})

			envFileChanged, currentEnvFileHash, err := stage2EnvFile(appConfig, p, &sidekickServer)
			if err != nil {
				p.Send(render.ErrorMsg{ErrorStr: err.Error()})
				return
			}
			p.Send(render.NextStageMsg{})

			if err := stage3BuildDockerImage(appConfig, p, &sidekickServer); err != nil {
				p.Send(render.ErrorMsg{ErrorStr: err.Error()})
				return
			}
			time.Sleep(time.Millisecond * 100)
			p.Send(render.NextStageMsg{})

			if err := stage4SaveDockerImage(appConfig, p); err != nil {
				p.Send(render.ErrorMsg{ErrorStr: err.Error()})
				return
			}
			time.Sleep(time.Millisecond * 200)
			p.Send(render.NextStageMsg{})

			if err := stage5MoveDockerImage(appConfig, p, &sidekickServer); err != nil {
				p.Send(render.ErrorMsg{ErrorStr: err.Error()})
				return
			}
			time.Sleep(time.Millisecond * 200)
			p.Send(render.NextStageMsg{})

			if err := stage6Deploy(sshClient, appConfig, envFileChanged, currentEnvFileHash, p, &sidekickServer); err != nil {
				p.Send(render.ErrorMsg{ErrorStr: err.Error()})
				return
			}

			time.Sleep(time.Millisecond * 500)
			p.Send(render.AllDoneMsg{Message: "🚀 Deployed successfully in " + time.Since(start).Round(time.Second).String() + ".\n" + "😎 View your app at https://" + appConfig.Url})
		}()

		if _, err := p.Run(); err != nil {
			fmt.Println("Error running program:", err)
			os.Exit(1)
		}

		// Post-deploy TLS validation — runs synchronously after TUI completes
		fmt.Println("\nValidating TLS certificate...")
		result, certErr := utils.ValidateTLSCertWithRetry(appConfig.Url)
		if certErr != nil {
			fmt.Printf("\n%s\n", fmt.Sprintf("⚠ Could not validate TLS certificate for %s: %s", appConfig.Url, certErr))
		} else {
			fmt.Printf("\n%s\n", utils.FormatCertCheckOutput(result))
		}
	},
}
