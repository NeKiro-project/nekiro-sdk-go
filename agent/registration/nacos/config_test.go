package nacos

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadConfigRequiresExactReleaseAndExplicitFreshness(t *testing.T) {
	values := validEnvironment("MY_AGENT")
	config, err := LoadConfig(mapLookup(values), "MY_AGENT", "runtime-b", "runtime-b-primary")
	if err != nil {
		t.Fatal(err)
	}
	if config.ReleaseID != "rel_runtime_b_1" || config.PortName != "a2a" || config.HeartbeatTimeout != 5*time.Second || config.IPDeleteTimeout != 10*time.Second {
		t.Fatalf("config=%#v", config)
	}
	for _, suffix := range []string{
		"RELEASE_ID", "CARD_DIGEST", "CANONICAL_ENDPOINT", "AUDIENCE", "NACOS_PORT_NAME",
		"NACOS_WEIGHT", "NACOS_HEARTBEAT_TIMEOUT_MS", "NACOS_IP_DELETE_TIMEOUT_MS",
	} {
		invalid := validEnvironment("MY_AGENT")
		delete(invalid, "MY_AGENT_"+suffix)
		if _, err := LoadConfig(mapLookup(invalid), "MY_AGENT", "runtime-b", "runtime-b-primary"); err == nil {
			t.Errorf("missing %s was accepted", suffix)
		}
	}
}

func TestLoadConfigAcceptsGenericSafePrefix(t *testing.T) {
	values := validEnvironment("PROVIDER_17")
	if _, err := LoadConfig(mapLookup(values), "PROVIDER_17", "agent", "instance"); err != nil {
		t.Fatal(err)
	}
	for _, prefix := range []string{"", "runtime_a", "RUNTIME_", "RUNTIME__A", "17_RUNTIME", "RUNTIME-A", strings.Repeat("A", 65)} {
		if _, err := LoadConfig(mapLookup(values), prefix, "agent", "instance"); err == nil {
			t.Errorf("unsafe prefix %q was accepted", prefix)
		}
	}
	if _, err := LoadConfig(nil, "PROVIDER_17", "agent", "instance"); err == nil {
		t.Fatal("nil lookup was accepted")
	}
	if _, err := LoadConfig(mapLookup(values), "PROVIDER_17", "bad agent", "instance"); err == nil {
		t.Fatal("invalid Agent ID was accepted")
	}
}

func TestLoadConfigDisabledModeRejectsNacosFields(t *testing.T) {
	for _, suffix := range nacosSuffixes {
		values := map[string]string{"MY_AGENT_REGISTRATION_MODE": ModeDisabled, "MY_AGENT_" + suffix: "value"}
		if _, err := LoadConfig(mapLookup(values), "MY_AGENT", "agent", "instance"); err == nil {
			t.Errorf("disabled registration accepted %s", suffix)
		}
	}
	config, err := LoadConfig(mapLookup(map[string]string{"MY_AGENT_REGISTRATION_MODE": ModeDisabled}), "MY_AGENT", "agent", "instance")
	if err != nil || config.Mode != ModeDisabled {
		t.Fatalf("disabled config=%#v error=%v", config, err)
	}
	config.APIOrigin = "http://nacos:8848/nacos"
	if err := config.Validate(); err == nil {
		t.Fatal("programmatic disabled config accepted Nacos fields")
	}
}

func TestLoadConfigRequiresExplicitHTTPSRegistrationTrust(t *testing.T) {
	values := validEnvironment("MY_AGENT")
	values["MY_AGENT_NACOS_API_ORIGIN"] = "https://nacos.internal:8848/nacos"
	values["MY_AGENT_NACOS_TLS_CA_FILE"] = filepath.Join(t.TempDir(), "ca.pem")
	values["MY_AGENT_NACOS_TLS_SERVER_NAME"] = "nacos.internal"
	config, err := LoadConfig(mapLookup(values), "MY_AGENT", "runtime-b", "runtime-b-primary")
	if err != nil || config.TLSCAFile == "" || config.TLSServerName != "nacos.internal" {
		t.Fatalf("HTTPS config=%#v error=%v", config, err)
	}

	for name, mutate := range map[string]func(map[string]string){
		"missing CA":          func(values map[string]string) { delete(values, "MY_AGENT_NACOS_TLS_CA_FILE") },
		"missing server name": func(values map[string]string) { delete(values, "MY_AGENT_NACOS_TLS_SERVER_NAME") },
		"relative CA":         func(values map[string]string) { values["MY_AGENT_NACOS_TLS_CA_FILE"] = "ca.pem" },
		"invalid server name": func(values map[string]string) { values["MY_AGENT_NACOS_TLS_SERVER_NAME"] = "nacos_internal" },
		"client cert only": func(values map[string]string) {
			values["MY_AGENT_NACOS_TLS_CLIENT_CERT_FILE"] = filepath.Join(t.TempDir(), "client.pem")
		},
		"client key only": func(values map[string]string) {
			values["MY_AGENT_NACOS_TLS_CLIENT_KEY_FILE"] = filepath.Join(t.TempDir(), "client-key.pem")
		},
	} {
		t.Run(name, func(t *testing.T) {
			invalid := cloneEnvironment(values)
			mutate(invalid)
			if _, err := LoadConfig(mapLookup(invalid), "MY_AGENT", "runtime-b", "runtime-b-primary"); err == nil {
				t.Fatal("invalid HTTPS registration trust was accepted")
			}
		})
	}
}

func TestLoadConfigAuthenticationAndNumericFields(t *testing.T) {
	accessToken := validEnvironment("MY_AGENT")
	accessToken["MY_AGENT_NACOS_AUTH_MODE"] = AuthAccessToken
	accessToken["MY_AGENT_NACOS_ACCESS_TOKEN"] = "token-value"
	config, err := LoadConfig(mapLookup(accessToken), "MY_AGENT", "runtime-b", "runtime-b-primary")
	if err != nil || config.AccessToken != "token-value" {
		t.Fatalf("access-token config=%#v error=%v", config, err)
	}

	for name, mutate := range map[string]func(map[string]string){
		"unknown mode":    func(values map[string]string) { values["MY_AGENT_REGISTRATION_MODE"] = "other" },
		"unknown auth":    func(values map[string]string) { values["MY_AGENT_NACOS_AUTH_MODE"] = "basic" },
		"token with none": func(values map[string]string) { values["MY_AGENT_NACOS_ACCESS_TOKEN"] = "token" },
		"missing token": func(values map[string]string) {
			values["MY_AGENT_NACOS_AUTH_MODE"] = AuthAccessToken
		},
		"signed port":         func(values map[string]string) { values["MY_AGENT_NACOS_ADVERTISED_PORT"] = "+8092" },
		"port out of range":   func(values map[string]string) { values["MY_AGENT_NACOS_ADVERTISED_PORT"] = "65536" },
		"fractional weight":   func(values map[string]string) { values["MY_AGENT_NACOS_WEIGHT"] = "1.5" },
		"short request":       func(values map[string]string) { values["MY_AGENT_NACOS_REQUEST_TIMEOUT_MS"] = "99" },
		"noncanonical IP":     func(values map[string]string) { values["MY_AGENT_NACOS_ADVERTISED_IP"] = "127.000.000.001" },
		"unsafe service name": func(values map[string]string) { values["MY_AGENT_NACOS_SERVICE_NAME"] = "bad name" },
	} {
		t.Run(name, func(t *testing.T) {
			values := validEnvironment("MY_AGENT")
			mutate(values)
			if _, err := LoadConfig(mapLookup(values), "MY_AGENT", "runtime-b", "runtime-b-primary"); err == nil {
				t.Fatal("invalid environment was accepted")
			}
		})
	}
}

func TestConfigValidateRejectsInvalidPolicyClasses(t *testing.T) {
	for name, mutate := range map[string]func(*Config){
		"identity":           func(config *Config) { config.InstanceID = "bad instance" },
		"mode":               func(config *Config) { config.Mode = "other" },
		"origin":             func(config *Config) { config.APIOrigin = "http://nacos:8848/other" },
		"target":             func(config *Config) { config.Audience = "http://other:8092" },
		"IP":                 func(config *Config) { config.AdvertisedIP = "localhost" },
		"port":               func(config *Config) { config.AdvertisedPort = 0 },
		"weight":             func(config *Config) { config.Weight = 1.5 },
		"heartbeat interval": func(config *Config) { config.HeartbeatInterval = 500 * time.Millisecond },
		"heartbeat timeout":  func(config *Config) { config.HeartbeatTimeout = time.Second },
		"delete timeout":     func(config *Config) { config.IPDeleteTimeout = 5 * time.Second },
		"request timeout":    func(config *Config) { config.RequestTimeout = 0 },
		"auth":               func(config *Config) { config.AuthMode = AuthAccessToken },
		"HTTP TLS":           func(config *Config) { config.TLSCAFile = filepath.Join(t.TempDir(), "ca.pem") },
	} {
		t.Run(name, func(t *testing.T) {
			config := validConfig("http://nacos:8848/nacos")
			mutate(&config)
			if err := config.Validate(); err == nil {
				t.Fatal("invalid configuration was accepted")
			}
		})
	}
}

func validEnvironment(prefix string) map[string]string {
	name := func(suffix string) string { return prefix + "_" + suffix }
	return map[string]string{
		name("REGISTRATION_MODE"): ModeNacos, name("AGENT_CARD_VERSION"): "1.0.0", name("RELEASE_ID"): "rel_runtime_b_1",
		name("CARD_DIGEST"):        "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		name("CANONICAL_ENDPOINT"): "http://runtime-b:8092/", name("AUDIENCE"): "http://runtime-b:8092",
		name("NACOS_API_ORIGIN"): "http://nacos:8848/nacos", name("NACOS_NAMESPACE_ID"): "public",
		name("NACOS_GROUP_NAME"): "NEKIRO", name("NACOS_SERVICE_NAME"): "runtime-b", name("NACOS_CLUSTER_NAME"): "DEFAULT",
		name("NACOS_PORT_NAME"): "a2a", name("NACOS_ADVERTISED_IP"): "127.0.0.1", name("NACOS_ADVERTISED_PORT"): "8092",
		name("NACOS_WEIGHT"): "1", name("NACOS_HEARTBEAT_INTERVAL_MS"): "1000", name("NACOS_HEARTBEAT_TIMEOUT_MS"): "5000",
		name("NACOS_IP_DELETE_TIMEOUT_MS"): "10000", name("NACOS_REQUEST_TIMEOUT_MS"): "1000", name("NACOS_AUTH_MODE"): AuthNone,
	}
}

func cloneEnvironment(values map[string]string) map[string]string {
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func mapLookup(values map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, ok := values[name]
		return value, ok
	}
}
