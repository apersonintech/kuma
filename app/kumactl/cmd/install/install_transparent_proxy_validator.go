package install

import (
	std_errors "errors"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/kumahq/kuma/pkg/config"
	"github.com/kumahq/kuma/pkg/core"
	kuma_log "github.com/kumahq/kuma/pkg/log"
	tproxy_config "github.com/kumahq/kuma/pkg/transparentproxy/config"
	tproxy_dp "github.com/kumahq/kuma/pkg/transparentproxy/config/dataplane"
	tproxy_consts "github.com/kumahq/kuma/pkg/transparentproxy/consts"
	tproxy_validate "github.com/kumahq/kuma/pkg/transparentproxy/validate"
	"github.com/kumahq/kuma/pkg/util/pointer"
)

const defaultLogName = "transparentproxy.validator"

func newInstallTransparentProxyValidator() *cobra.Command {
	ipFamilyMode := tproxy_config.IPFamilyModeDualStack
	serverPort := tproxy_validate.ServerPort

	var tpCfgValues []string

	cmd := &cobra.Command{
		Use:   "transparent-proxy-validator",
		Short: "Validates if transparent proxy has been set up successfully",
		Long: `Validates the transparent proxy setup by testing if the applied 
iptables rules are working correctly onto the pod.

Follow the following steps to validate:
 1) install the transparent proxy using 'kumactl install transparent-proxy'
 2) run this command

The result will be shown as text in stdout as well as the exit code.
`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := core.NewLoggerTo(os.Stdout, kuma_log.InfoLevel).WithName(defaultLogName)

			var tpCfg *tproxy_dp.DataplaneConfig
			if len(tpCfgValues) > 0 {
				if runtime.GOOS != "linux" {
					return errors.New("transparent proxy is supported only on Linux systems")
				}

				tpCfg = pointer.To(tproxy_dp.DefaultDataplaneConfig())
				if err := config.NewLoader(tpCfg).WithValidation().Load(cmd.InOrStdin(), tpCfgValues...); err != nil {
					return errors.Wrap(err, "failed to load transparent proxy configuration from provided input")
				}

				ipFamilyMode = tpCfg.IPFamilyMode
				serverPort = uint16(tpCfg.Redirect.Inbound.Port)
			}

			hasLocalIPv6Addr, _ := tproxy_config.HasLocalIPv6()
			validateOnlyIPv4 := ipFamilyMode == tproxy_config.IPFamilyModeIPv4

			validate := func(ipv6 bool) error {
				if ipv6 && !hasLocalIPv6Addr || validateOnlyIPv4 {
					return nil
				}

				logger := logger.WithName(strings.ToLower(tproxy_consts.IPTypeMap[ipv6]))
				validator := tproxy_validate.NewValidator(ipv6, serverPort, logger)
				exitC := make(chan struct{})

				if _, err := validator.RunServer(cmd.Context(), exitC); err != nil {
					return err
				}

				// by using 0, we make the client to generate a random port to connect verifying
				// the iptables rules are working
				return validator.RunClient(cmd.Context(), 0, exitC)
			}

			return errors.Wrap(
				std_errors.Join(validate(false), validate(true)),
				"validation failed",
			)
		},
	}

	cmd.Flags().Var(
		&ipFamilyMode,
		"ip-family-mode",
		fmt.Sprintf(
			"specify the IP family mode for traffic redirection when setting up the transparent proxy; accepted values: %s",
			tproxy_config.AllowedIPFamilyModes(),
		),
	)

	cmd.Flags().Uint16Var(
		&serverPort,
		"validation-server-port",
		serverPort,
		"port number for the validation server to listen on",
	)

	cmd.Flags().StringArrayVar(
		&tpCfgValues,
		"transparent-proxy-config",
		tpCfgValues,
		"Transparent proxy configuration. This flag can be repeated. Each value can be:\n"+
			"- a comma-separated list of file paths\n"+
			"- a dash '-' to read from STDIN\n"+
			"Later values override earlier ones when merging.",
	)

	return cmd
}
