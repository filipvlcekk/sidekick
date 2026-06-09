package certstatus

import (
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"github.com/mightymoud/sidekick/utils"
	"github.com/spf13/cobra"
)

var CertStatusCmd = &cobra.Command{
	Use:   "cert-status",
	Short: "Check TLS certificate status for deployed apps",
	Long:  `Connects to your VPS, inspects Traefik logs for ACME errors, and validates TLS certificates for all deployed apps.`,
	Run: func(cmd *cobra.Command, args []string) {
		config, err := utils.GetSidekickConfigFromCmdContext(cmd)
		if err != nil {
			log.Fatalf("Failed to load config: %s", err)
		}

		serverName, _ := cmd.Flags().GetString("server")
		appFilter, _ := cmd.Flags().GetString("app")

		// Resolve server
		var server utils.SidekickServer
		if serverName != "" {
			server, err = config.FindServer(serverName)
			if err != nil {
				log.Fatalf("Server '%s' not found in config", serverName)
			}
		} else {
			ctx, err := config.FindContext(config.CurrentContext)
			if err != nil {
				log.Fatalf("No current context set. Use --server flag or run 'sidekick init'")
			}
			server, err = config.FindServer(ctx.Server)
			if err != nil {
				log.Fatalf("Server for context '%s' not found", config.CurrentContext)
			}
		}

		// SSH into server
		client, err := utils.Login(server.Address, "sidekick")
		if err != nil {
			log.Fatalf("SSH connection failed: %s", err)
		}
		defer client.Close()

		fmt.Printf("Certificate Status for server \"%s\" (%s)\n", server.Name, server.Address)
		fmt.Println(strings.Repeat("─", 50))
		fmt.Println()

		// Get Traefik ACME logs
		outChan, _, err := utils.RunCommand(client, "cd traefik && sudo docker compose -p sidekick logs traefik-service 2>&1 | grep -i 'acme\\|certificate\\|error' | tail -20")
		var acmeLogs string
		if err == nil {
			acmeLogs = <-outChan
		}

		// Check acme.json for per-domain cert entries
		outChan, _, err = utils.RunCommand(client, `cat traefik/ssl-certs/acme.json 2>/dev/null || echo "{}"`)
		var acmeJSON string
		if err == nil {
			acmeJSON = <-outChan
		}

		// List deployed apps (containers with traefik labels)
		outChan, _, err = utils.RunCommand(client, `docker ps --format '{{.Labels}}' | grep -oP 'traefik\.http\.routers\.\K[^.]+(?=\.rule)' | sort -u`)
		if err != nil {
			log.Fatalf("Failed to list apps: %s", err)
		}
		appsOutput := <-outChan
		apps := strings.Split(strings.TrimSpace(appsOutput), "\n")

		if len(apps) == 0 || (len(apps) == 1 && apps[0] == "") {
			fmt.Println("No deployed apps found on this server")
			return
		}

		// Get domains for each app
		for _, app := range apps {
			if appFilter != "" && app != appFilter {
				continue
			}

			outChan, _, err = utils.RunCommand(client, fmt.Sprintf(`docker ps --format '{{.Labels}}' | grep -oP 'traefik\.http\.routers\.%s\.rule=Host\(\x60\K[^\x60]+' | head -1`, app))
			if err != nil {
				fmt.Printf("%s\n  ✗ Could not determine domain\n\n", app)
				continue
			}
			domain := strings.TrimSpace(<-outChan)
			if domain == "" {
				fmt.Printf("%s\n  ✗ Could not determine domain\n\n", app)
				continue
			}

			fmt.Printf("%s\n", domain)

			coverage := summarizeCertificateCoverage(server, domain, acmeJSON)
			switch {
			case coverage.UsesWildcard && !coverage.DomainWithinZone:
				fmt.Printf("  ✗ Wildcard coverage: app domain is outside wildcard domain %s\n", server.WildcardDomain)
			case coverage.ACMEEntryFound && coverage.UsesWildcard:
				fmt.Printf("  ✓ ACME storage: wildcard certificate coverage found\n")
			case coverage.ACMEEntryFound:
				fmt.Printf("  ✓ ACME storage: certificate entry found\n")
			case coverage.UsesWildcard:
				fmt.Printf("  ✗ ACME storage: no wildcard certificate coverage found\n")
			default:
				fmt.Printf("  ✗ ACME storage: no certificate entry for this domain\n")
			}

			// Validate TLS cert against the known server address while preserving domain SNI.
			result, err := utils.ValidateTLSCertAtAddress(domain, net.JoinHostPort(server.Address, "443"))
			if err != nil {
				fmt.Printf("  ✗ Connection failed: %s\n", err)
			} else if result.Valid {
				expDays := int(result.ExpiresAt.Sub(time.Now()).Hours() / 24)
				fmt.Printf("  ✓ Certificate: %s\n", result.Issuer)
				fmt.Printf("  ✓ Expires: %s (%d days)\n", result.ExpiresAt.Format("2006-01-02"), expDays)
			} else {
				fmt.Printf("  ✗ Certificate: %s", result.Issuer)
				if result.IsSelfSigned {
					fmt.Printf(" (self-signed)")
				}
				fmt.Println()
			}

			// Check ACME logs for this domain
			if acmeLogs != "" {
				relevantLogs := filterLogsForDomain(acmeLogs, domain)
				if relevantLogs != "" {
					fmt.Printf("  ✗ ACME logs: %s\n", relevantLogs)
				} else {
					fmt.Printf("  ✓ ACME logs: no errors\n")
				}
			}

			fmt.Printf("  %s\n", utils.FormatDNSCheckOutputForServer(utils.CheckPublicDNS(domain, server.Address), server))
			fmt.Println()
		}
	},
}

type certificateCoverageStatus struct {
	UsesWildcard     bool
	DomainWithinZone bool
	ACMEEntryFound   bool
}

func filterLogsForDomain(logs, domain string) string {
	lines := strings.Split(logs, "\n")
	var relevant []string
	for _, line := range lines {
		lower := strings.ToLower(line)
		if strings.Contains(lower, strings.ToLower(domain)) &&
			(strings.Contains(lower, "error") || strings.Contains(lower, "unable")) {
			relevant = append(relevant, strings.TrimSpace(line))
		}
	}
	if len(relevant) > 0 {
		return relevant[len(relevant)-1] // most recent error
	}
	return ""
}

func acmeEntryExists(acmeJSON, domain string) bool {
	return strings.Contains(acmeJSON, domain)
}

func summarizeCertificateCoverage(server utils.SidekickServer, domain, acmeJSON string) certificateCoverageStatus {
	normalizedServer := server
	utils.NormalizeSidekickServer(&normalizedServer)

	if normalizedServer.CertificateMode == utils.CertificateModeWildcard {
		withinZone := utils.IsHostnameWithinWildcardDomain(domain, normalizedServer.WildcardDomain)
		if !withinZone {
			return certificateCoverageStatus{
				UsesWildcard:     true,
				DomainWithinZone: false,
				ACMEEntryFound:   false,
			}
		}

		return certificateCoverageStatus{
			UsesWildcard:     true,
			DomainWithinZone: true,
			ACMEEntryFound: acmeEntryExists(acmeJSON, normalizedServer.WildcardDomain) ||
				acmeEntryExists(acmeJSON, "*."+normalizedServer.WildcardDomain) ||
				acmeEntryExists(acmeJSON, domain),
		}
	}

	return certificateCoverageStatus{
		DomainWithinZone: true,
		ACMEEntryFound:   acmeEntryExists(acmeJSON, domain),
	}
}

func init() {
	CertStatusCmd.Flags().String("server", "", "Target server name (defaults to current context)")
	CertStatusCmd.Flags().String("app", "", "Check only a specific app")
}
