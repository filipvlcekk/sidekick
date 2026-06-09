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
package initialize

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/docker/docker/pkg/namesgenerator"
	"github.com/mightymoud/sidekick/render"
	"github.com/mightymoud/sidekick/utils"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/crypto/ssh"
)

func stage1LocalReqs() error {
	if _, err := exec.LookPath("sops"); err != nil {
		cmd := exec.Command("brew", "install", "sops")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to install sops: %w", err)
		}
	}
	if _, err := exec.LookPath("age"); err != nil {
		cmd := exec.Command("brew", "install", "age")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to install age: %w", err)
		}
	}
	return nil
}

func stage2Login(server string) (*ssh.Client, string, error) {
	users := []string{"root", "sidekick"}
	for _, user := range users {
		client, err := utils.Login(server, user)
		if err == nil {
			return client, user, nil
		}
	}
	return nil, "", fmt.Errorf("unable to establish SSH connection")
}

func stage3UserSetup(client *ssh.Client, loggedInUser string) error {
	hasSidekickUser := true
	outChan, _, err := utils.RunCommand(client, "id -u sidekick")
	if err != nil {
		hasSidekickUser = false
	} else {
		output := <-outChan
		if output == "" {
			hasSidekickUser = false
		}
	}

	if !hasSidekickUser && loggedInUser == "root" {
		if err := utils.RunStage(client, utils.UsersetupStage); err != nil {
			return err
		}
	}
	return nil
}

func stage4VPSSetup(client *ssh.Client, p *tea.Program, server *utils.SidekickServer) error {
	// get the linux distro
	outChan, _, _ := utils.RunCommand(client, "grep '^ID=' /etc/os-release | awk -F'=' '{print $2}'")
	linuxDistro := <-outChan
	server.Distro = linuxDistro

	// get docker platform id
	cmdOutChan, _, _ := utils.RunCommand(client, "uname -m")
	arch := <-cmdOutChan
	if arch == "x86_64" {
		server.PlatformId = "linux/amd64"
	}
	if arch == "aarch64" {
		server.PlatformId = "linux/arm64"
	}

	if err := utils.RunCommandsWithTUIHook(client, utils.SetupStage.Commands, p); err != nil {
		return err
	}

	if server.PublicKey == "" || server.SecretKey == "" {
		cmd := exec.Command("age-keygen")
		output, err := cmd.Output()
		if err != nil {
			return err
		}
		outStr := string(output)
		lines := strings.Split(outStr, "\n")
		if len(lines) >= 3 {
			server.SecretKey = lines[2]
			parts := strings.Split(lines[1], ":")
			if len(parts) > 1 {
				server.PublicKey = strings.ReplaceAll(parts[1], " ", "")
			}
		}
	}
	return nil
}

func stage5Docker(client *ssh.Client, p *tea.Program) error {
	dockerReady := false
	outChan, _, err := utils.RunCommand(client, `command -v docker &> /dev/null && command -v docker compose &> /dev/null && echo "1" || echo "0"`)
	if err == nil {
		output := <-outChan
		if output == "1" {
			dockerReady = true
		}
	}

	if !dockerReady {
		if err := utils.RunCommandsWithTUIHook(client, utils.DockerStage.Commands, p); err != nil {
			return err
		}
	}
	return nil
}

func stage6Traefik(client *ssh.Client, email string, provider utils.DNSProvider, envVars map[string]string, existingServer, requestedServer utils.SidekickServer, skipPrompts bool, p *tea.Program) error {
	traefikSetup := false
	existingHTTP01 := false
	existingDNS01Provider := ""

	outChan, _, err := utils.RunCommand(client, `[ -d "traefik" ] && echo "1" || echo "0"`)
	if err == nil {
		output := <-outChan
		if output == "1" {
			traefikSetup = true
			// Check if existing setup uses HTTP-01 or DNS-01
			outChan, _, err = utils.RunCommand(client, `grep -o "httpchallenge" traefik/docker-compose.yml 2>/dev/null || echo ""`)
			if err == nil {
				output = <-outChan
				if strings.Contains(output, "httpchallenge") {
					existingHTTP01 = true
				}
			}
			// Check for existing DNS provider
			outChan, _, err = utils.RunCommand(client, `grep -oP 'dnschallenge\.provider=\K\S+' traefik/docker-compose.yml 2>/dev/null || echo ""`)
			if err == nil {
				output = <-outChan
				existingDNS01Provider = strings.TrimSpace(output)
			}
		}
	}

	if !traefikSetup {
		// Fresh install
		traefikStage := utils.GetTraefikStage(email, provider, envVars)
		if err := utils.RunCommandsWithTUIHook(client, traefikStage.Commands, p); err != nil {
			return err
		}
		return nil
	}

	// Migration: HTTP-01 → DNS-01
	if existingHTTP01 {
		if !skipPrompts {
			confirm := render.GenerateTextQuestion("Existing HTTP-01 setup detected. Migrate to DNS-01? (y/n)", "y", "")
			if strings.ToLower(confirm) != "y" {
				fmt.Println("Skipping Traefik migration")
				return nil
			}
		}
		traefikStage := utils.GetTraefikMigrationStage(email, provider, envVars)
		if err := utils.RunCommandsWithTUIHook(client, traefikStage.Commands, p); err != nil {
			return err
		}
		return nil
	}

	// Migration: DNS-01 provider change
	if existingDNS01Provider != "" && existingDNS01Provider != provider.TraefikName {
		if !skipPrompts {
			confirm := render.GenerateTextQuestion(
				fmt.Sprintf("Current DNS provider is %s. Switch to %s? (y/n)", existingDNS01Provider, provider.TraefikName), "y", "")
			if strings.ToLower(confirm) != "y" {
				fmt.Println("Skipping DNS provider change")
				return nil
			}
		}
		traefikStage := utils.GetTraefikMigrationStage(email, provider, envVars)
		if err := utils.RunCommandsWithTUIHook(client, traefikStage.Commands, p); err != nil {
			return err
		}
		return nil
	}

	if shouldRewriteTraefikForCertificateMode(
		existingServer.CertificateMode,
		requestedServer.CertificateMode,
		existingServer.WildcardDomain,
		requestedServer.WildcardDomain,
	) {
		if !skipPrompts {
			confirm := render.GenerateTextQuestion(
				fmt.Sprintf(
					"Current certification mode is %s. Rewrite Traefik for %s mode? (y/n)",
					utils.NormalizeCertificateMode(existingServer.CertificateMode),
					utils.NormalizeCertificateMode(requestedServer.CertificateMode),
				),
				"y",
				"",
			)
			if strings.ToLower(confirm) != "y" {
				fmt.Println("Skipping certification mode migration")
				return nil
			}
		}
		traefikStage := utils.GetTraefikMigrationStage(email, provider, envVars)
		if err := utils.RunCommandsWithTUIHook(client, traefikStage.Commands, p); err != nil {
			return err
		}
		return nil
	}

	// Already configured with same provider — skip
	return nil
}

func applyCertificateSettings(server utils.SidekickServer, mode, wildcardDomain string) (utils.SidekickServer, error) {
	server.CertificateMode = mode
	server.WildcardDomain = wildcardDomain
	utils.NormalizeSidekickServer(&server)

	if err := utils.ValidateCertificateModeConfig(server.CertificateMode, server.WildcardDomain); err != nil {
		return server, err
	}

	if server.CertificateMode == utils.CertificateModePerHost {
		server.WildcardDomain = ""
	}

	return server, nil
}

func wildcardInitGuidance(domain string) string {
	normalizedDomain := domain
	serverPattern := "*.example.com"
	if normalizedDomain != "" {
		serverPattern = "*." + normalizedDomain
	}

	return fmt.Sprintf(
		"Wildcard DNS is optional but recommended.\nYou can keep using per-app DNS records for each hostname, or create one wildcard DNS record such as %s (for example %s) that points to this server.",
		serverPattern,
		"*.example.com",
	)
}

func shouldRewriteTraefikForCertificateMode(existingMode, requestedMode, existingWildcardDomain, requestedWildcardDomain string) bool {
	existingServer := utils.SidekickServer{
		CertificateMode: existingMode,
		WildcardDomain:  existingWildcardDomain,
	}
	requestedServer := utils.SidekickServer{
		CertificateMode: requestedMode,
		WildcardDomain:  requestedWildcardDomain,
	}

	utils.NormalizeSidekickServer(&existingServer)
	utils.NormalizeSidekickServer(&requestedServer)

	if existingServer.CertificateMode != requestedServer.CertificateMode {
		return true
	}

	if requestedServer.CertificateMode == utils.CertificateModeWildcard &&
		existingServer.WildcardDomain != requestedServer.WildcardDomain {
		return true
	}

	return false
}

var InitCmd = &cobra.Command{
	Use:   "init",
	Short: "Init sidekick CLI and configure your VPS to host your apps",
	Long: `This command will run you through the setup steps to get sidekick loaded on your VPS.
		You wil need to provide your VPS IPv4 address and a registry to host your docker images.
		`,
	Run: func(cmd *cobra.Command, args []string) {
		start := time.Now()

		config, err := utils.GetSidekickConfigFromCmdContext(cmd)
		if err != nil {
			log.Fatalf("%s", err)
		}

		skipPromptsFlag, _ := cmd.Flags().GetBool("yes")
		server, _ := cmd.Flags().GetString("server")
		certEmail, _ := cmd.Flags().GetString("email")
		name, _ := cmd.Flags().GetString("name")
		certificateModeFlag, _ := cmd.Flags().GetString("certificate-mode")
		wildcardDomainFlag, _ := cmd.Flags().GetString("wildcard-domain")

		if name == "" {
			randomName := namesgenerator.GetRandomName(0)
			name = render.GenerateTextQuestion("Please enter a name for your VPS", randomName, "")
		}

		if server == "" {
			server = render.GenerateTextQuestion("Please enter the IPv4 Address of your VPS", "", "")
			if !utils.IsValidIPAddress(server) {
				log.Fatalf("You entered an incorrect IP Address - %s", server)
			}
		}

		if certEmail == "" {
			certEmail = render.GenerateTextQuestion("Please enter an email for use with TLS certs", "", "")
			if certEmail == "" {
				log.Fatalf("An email is needed before you proceed")
			}
		}

		// DNS provider selection
		var selectedProvider utils.DNSProvider
		dnsProviderFlag, _ := cmd.Flags().GetString("dns-provider")
		dnsEnvFlags, _ := cmd.Flags().GetStringArray("dns-env")

		if dnsProviderFlag != "" {
			var err error
			selectedProvider, err = utils.GetDNSProvider(dnsProviderFlag)
			if err != nil {
				log.Fatalf("Unknown DNS provider: %s. Use one of: cloudflare, route53, digitalocean, hetzner, godaddy", dnsProviderFlag)
			}
		} else {
			// Interactive provider selection
			providers := utils.GetAllDNSProviders()
			options := make([]huh.Option[int], 0, len(providers))
			for i, p := range providers {
				options = append(options, huh.NewOption(fmt.Sprintf("%s — %s", p.Name, p.Description), i))
			}

			var selectedIndex int
			form := huh.NewForm(
				huh.NewGroup(
					huh.NewSelect[int]().
						Title("Select your DNS provider for Let's Encrypt certificates").
						Options(options...).
						Value(&selectedIndex),
				),
			)
			if err := form.Run(); err != nil {
				log.Fatalf("DNS provider selection failed: %s", err)
			}
			selectedProvider = providers[selectedIndex]
		}

		// Collect DNS credentials
		dnsEnvVars := make(map[string]string)
		if len(dnsEnvFlags) > 0 {
			// Parse from flags: --dns-env KEY=VALUE
			for _, env := range dnsEnvFlags {
				parts := strings.SplitN(env, "=", 2)
				if len(parts) != 2 {
					log.Fatalf("Invalid --dns-env format: %s (expected KEY=VALUE)", env)
				}
				dnsEnvVars[parts[0]] = parts[1]
			}
		} else {
			// Interactive credential prompts
			for _, envVar := range selectedProvider.EnvVars {
				value := render.GenerateTextQuestion(fmt.Sprintf("Enter %s", envVar), "", "")
				if value == "" {
					log.Fatalf("%s is required for %s", envVar, selectedProvider.Name)
				}
				dnsEnvVars[envVar] = value
			}
		}

		// Validate all required env vars are provided
		for _, required := range selectedProvider.EnvVars {
			if _, ok := dnsEnvVars[required]; !ok {
				log.Fatalf("Missing required env var %s for provider %s", required, selectedProvider.Name)
			}
		}

		sidekickServer, err := config.FindServer(name)
		if err != nil {
			sidekickServer = utils.SidekickServer{
				Name:      name,
				Address:   server,
				CertEmail: certEmail,
			}
		}
		existingServerConfig := sidekickServer

		if sidekickServer.Name == name && sidekickServer.Address != server && sidekickServer.PublicKey != "" && !skipPromptsFlag {
			confirm := render.GenerateTextQuestion(fmt.Sprintf("The server '%s' was previously setup with Sidekick using a different address. Would you like to overwrite the settings? (y/n)", sidekickServer.Name), "n", "")
			if strings.ToLower(confirm) != "y" {
				fmt.Println("\nYou can use a different server name to complete the setup")
				os.Exit(0)
			}
		}

		sidekickServer.Address = server
		sidekickServer.CertEmail = certEmail
		sidekickServer.DNSProvider = selectedProvider.TraefikName

		selectedCertificateMode := utils.NormalizeCertificateMode(certificateModeFlag)
		if certificateModeFlag == "" {
			selectedCertificateMode = utils.NormalizeCertificateMode(sidekickServer.CertificateMode)
			options := []huh.Option[string]{
				huh.NewOption("Per-host", utils.CertificateModePerHost),
				huh.NewOption("Wildcard", utils.CertificateModeWildcard),
			}

			form := huh.NewForm(
				huh.NewGroup(
					huh.NewSelect[string]().
						Title("Select your certification mode").
						Options(options...).
						Value(&selectedCertificateMode),
				),
			)
			if err := form.Run(); err != nil {
				log.Fatalf("Certification mode selection failed: %s", err)
			}
		}

		selectedWildcardDomain := wildcardDomainFlag
		if selectedCertificateMode == utils.CertificateModeWildcard && selectedWildcardDomain == "" {
			selectedWildcardDomain = render.GenerateTextQuestion("Enter the wildcard domain for this server", sidekickServer.WildcardDomain, "")
		}

		sidekickServer, err = applyCertificateSettings(sidekickServer, selectedCertificateMode, selectedWildcardDomain)
		if err != nil {
			log.Fatalf("Invalid certification mode settings: %s", err)
		}
		if sidekickServer.CertificateMode == utils.CertificateModeWildcard {
			fmt.Println(wildcardInitGuidance(sidekickServer.WildcardDomain))
		}

		cmdStages := []render.Stage{
			render.MakeStage("Setting up your local env", "Installed local requirements successfully", false),
			render.MakeStage("Logging in to VPS", "Logged in successfully", false),
			render.MakeStage("Adding user Sidekick", "User Sidekick added successfully", false),
			render.MakeStage("Setting up VPS", "VPS setup successfully", true),
			render.MakeStage("Setting up Docker", "Docker setup successfully", true),
			render.MakeStage("Setting up Traefik", "Traefik setup successfully", true),
		}

		p := tea.NewProgram(render.TuiModel{
			Stages:      cmdStages,
			BannerMsg:   "Sidekick booting up! 🚀",
			ActiveIndex: 0,
			Quitting:    false,
			AllDone:     false,
		})

		utils.Login(server, "root")

		go func() {
			if err := stage1LocalReqs(); err != nil {
				p.Send(render.ErrorMsg{ErrorStr: fmt.Sprintf("Local requirements check failed: %s", err)})
				return
			}
			time.Sleep(time.Millisecond * 100)
			p.Send(render.NextStageMsg{})

			sshClient, loggedInUser, err := stage2Login(server)
			if err != nil {
				p.Send(render.ErrorMsg{ErrorStr: fmt.Sprintf("Login failed: %s", err)})
				return
			}
			time.Sleep(time.Millisecond * 100)
			p.Send(render.NextStageMsg{})

			if err := stage3UserSetup(sshClient, loggedInUser); err != nil {
				p.Send(render.ErrorMsg{ErrorStr: fmt.Sprintf("User setup failed: %s", err)})
				return
			}

			sidekickClient, err := utils.Login(server, "sidekick")
			if err != nil {
				p.Send(render.ErrorMsg{ErrorStr: fmt.Sprintf("Failed to login as sidekick: %s", err)})
				return
			}
			time.Sleep(time.Millisecond * 100)
			p.Send(render.NextStageMsg{})

			if err := stage4VPSSetup(sidekickClient, p, &sidekickServer); err != nil {
				p.Send(render.ErrorMsg{ErrorStr: fmt.Sprintf("VPS setup failed: %s", err)})
				return
			}
			time.Sleep(time.Millisecond * 100)
			p.Send(render.NextStageMsg{})

			if err := stage5Docker(sidekickClient, p); err != nil {
				p.Send(render.ErrorMsg{ErrorStr: fmt.Sprintf("Docker setup failed: %s", err)})
				return
			}
			time.Sleep(time.Millisecond * 100)
			p.Send(render.NextStageMsg{})

			if err := stage6Traefik(sidekickClient, certEmail, selectedProvider, dnsEnvVars, existingServerConfig, sidekickServer, skipPromptsFlag, p); err != nil {
				p.Send(render.ErrorMsg{ErrorStr: fmt.Sprintf("Traefik setup failed: %s", err)})
				return
			}

			config.AddOrReplaceServer(sidekickServer)
			newContext := utils.SidekickContext{Name: sidekickServer.Name, Server: sidekickServer.Name}
			config.AddOrReplaceContext(newContext)
			config.CurrentContext = newContext.Name

			if err := config.Save(viper.GetString("config")); err != nil {
				p.Send(render.ErrorMsg{ErrorStr: fmt.Sprintf("Failed to write config: %s", err)})
				return
			}

			p.Send(render.AllDoneMsg{Message: "VPS Setup Done in " + time.Since(start).Round(time.Second).String() + "," + "\n" + "Your VPS is ready! You can now run Sidekick launch in your app folder"})
		}()

		if _, err := p.Run(); err != nil {
			fmt.Println("Error running program:", err)
			os.Exit(1)
		}
	},
}

func init() {
	InitCmd.Flags().StringP("server", "s", "", "Set the IP address of your Server")
	InitCmd.Flags().StringP("email", "e", "", "An email address to be used for SSL certs")
	InitCmd.Flags().StringP("name", "n", "", "Set the name of your Server")
	InitCmd.Flags().BoolP("yes", "y", false, "Skip all validation prompts")
	InitCmd.Flags().String("dns-provider", "", "DNS provider for ACME DNS-01 challenge (cloudflare, route53, digitalocean, hetzner, godaddy)")
	InitCmd.Flags().StringArray("dns-env", []string{}, "DNS provider environment variable (KEY=VALUE, can be repeated)")
	InitCmd.Flags().String("certificate-mode", "", "Certificate mode for Traefik TLS (per-host or wildcard)")
	InitCmd.Flags().String("wildcard-domain", "", "Base wildcard DNS zone for wildcard certificate mode, e.g. saola.cz")
}
