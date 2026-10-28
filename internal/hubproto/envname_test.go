package hubproto

import (
	"errors"
	"testing"
)

// Which rule a refused name broke decides what the hub tells the person, so the fault is part of the contract.
func TestCheckEnvNameFaults(t *testing.T) {
	cases := []struct {
		name  string
		fault EnvFault // 0: accepted
	}{
		{"GH_TOKEN", 0}, {"HF_TOKEN", 0}, {"SENTRY_DSN", 0}, {"DB_USER", 0}, {"_X_TOKEN", 0},
		{"lower_token", EnvMalformed}, {"1_TOKEN", EnvMalformed}, {"A-B_TOKEN", EnvMalformed},
		// Addresses are not secrets: none of them is accepted, whatever tool it points at which server.
		{"GITLAB_HOST", EnvNotSecret}, {"REGISTRY_URL", EnvNotSecret}, {"DB_HOST", EnvNotSecret}, {"SERVICE_URL", EnvNotSecret},
		// Names a tool reads to fetch code from elsewhere, or to run it.
		{"GH_HOST", EnvReserved}, {"GOPROXY", EnvReserved}, {"GOFLAGS", EnvReserved}, {"HF_ENDPOINT", EnvReserved},
		{"PIP_INDEX_URL", EnvReserved}, {"PIP_TOKEN", EnvReserved}, {"UV_INDEX_URL", EnvReserved}, {"UV_PUBLISH_TOKEN", EnvReserved},
		{"DOCKER_HOST", EnvReserved}, {"DOCKER_AUTH_KEY", EnvReserved}, {"NPM_CONFIG_REGISTRY", EnvReserved},
		{"NPM_CONFIG__AUTH_TOKEN", EnvReserved}, {"CARGO_REGISTRY_TOKEN", EnvReserved}, {"CARGO_HOME", EnvReserved},
		{"CONDA_CHANNEL_ALIAS", EnvReserved}, {"CONDA_TOKEN", EnvReserved},
		{"GIT_SSH_COMMAND", EnvReserved}, {"GIT_TOKEN", EnvReserved}, {"PAGER", EnvReserved}, {"LESSOPEN", EnvReserved},
		{"SSL_CERT_FILE", EnvReserved}, {"NODE_EXTRA_CA_CERTS", EnvReserved}, {"AWS_CA_KEY", EnvReserved}, {"FTP_PROXY", EnvReserved},
		{"PYTHONPATH", EnvReserved}, {"PYTHON_KEY", EnvReserved}, {"PERL5OPT", EnvReserved}, {"RUBYOPT", EnvReserved},
		{"JAVA_TOOL_OPTIONS", EnvReserved}, {"_JAVA_OPTIONS", EnvReserved}, {"GCONV_PATH", EnvReserved}, {"PS4", EnvReserved},
		{"BROWSER", EnvReserved}, {"BASH_ENV", EnvReserved}, {"LD_PRELOAD", EnvReserved}, {"ANTHROPIC_API_KEY", EnvReserved},
		// What is allowed in shape but is not a secret by its name.
		{"DEBUG", EnvNotSecret}, {"NODE_ENV", EnvNotSecret}, {"TOKEN_FILE", EnvNotSecret},
	}
	for _, tc := range cases {
		err := CheckEnvName(tc.name)
		if tc.fault == 0 {
			if err != nil {
				t.Errorf("%q refused: %v", tc.name, err)
			}
			continue
		}
		var bad *EnvNameError
		if !errors.As(err, &bad) {
			t.Errorf("%q accepted, want fault %d", tc.name, tc.fault)
			continue
		}
		if bad.Fault != tc.fault {
			t.Errorf("%q: fault %d (%v), want %d", tc.name, bad.Fault, err, tc.fault)
		}
	}
}
