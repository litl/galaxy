package auth

// From: https://github.com/dotcloud/docker/blob/master/auth/auth.go

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strings"
)

// Where we store the config file
const CONFIGFILE = ".dockercfg"

// Only used for user auth + account creation
const INDEXSERVER = "https://index.docker.io/v1/"

//const INDEXSERVER = "https://indexstaging-docker.dotcloud.com/v1/"

var (
	ErrConfigFileMissing = errors.New("The Auth config file is missing")
)

type AuthConfig struct {
	Username      string `json:"username,omitempty"`
	Password      string `json:"password,omitempty"`
	Auth          string `json:"auth"`
	Email         string `json:"email"`
	ServerAddress string `json:"serveraddress,omitempty"`
}

type ConfigFile struct {
	Configs  map[string]AuthConfig `json:"configs,omitempty"`
	rootPath string
}

func IndexServerAddress() string {
	return INDEXSERVER
}

// decode the auth string
func decodeAuth(authStr string) (string, string, error) {
	decLen := base64.StdEncoding.DecodedLen(len(authStr))
	decoded := make([]byte, decLen)
	authByte := []byte(authStr)
	n, err := base64.StdEncoding.Decode(decoded, authByte)
	if err != nil {
		return "", "", err
	}
	if n > decLen {
		return "", "", fmt.Errorf("Something went wrong decoding auth config")
	}
	arr := strings.SplitN(string(decoded), ":", 2)
	if len(arr) != 2 {
		return "", "", fmt.Errorf("Invalid auth configuration file")
	}
	password := strings.Trim(arr[1], "\x00")
	return arr[0], password, nil
}

// load up the auth config information and return values
// FIXME: use the internal golang config parser
func LoadConfig(rootPath string) (*ConfigFile, error) {
	configFile := ConfigFile{Configs: make(map[string]AuthConfig), rootPath: rootPath}
	confFile := path.Join(rootPath, CONFIGFILE)
	if _, err := os.Stat(confFile); err != nil {
		return &configFile, nil //missing file is not an error
	}
	b, err := ioutil.ReadFile(confFile)
	if err != nil {
		return &configFile, err
	}

	if err := json.Unmarshal(b, &configFile.Configs); err != nil {
		arr := strings.Split(string(b), "\n")
		if len(arr) < 2 {
			return &configFile, fmt.Errorf("The Auth config file is empty")
		}
		authConfig := AuthConfig{}
		origAuth := strings.Split(arr[0], " = ")
		if len(origAuth) != 2 {
			return &configFile, fmt.Errorf("Invalid Auth config file")
		}
		authConfig.Username, authConfig.Password, err = decodeAuth(origAuth[1])
		if err != nil {
			return &configFile, err
		}
		origEmail := strings.Split(arr[1], " = ")
		if len(origEmail) != 2 {
			return &configFile, fmt.Errorf("Invalid Auth config file")
		}
		authConfig.Email = origEmail[1]
		authConfig.ServerAddress = IndexServerAddress()
		configFile.Configs[IndexServerAddress()] = authConfig
	} else {
		for k, authConfig := range configFile.Configs {
			authConfig.Username, authConfig.Password, err = decodeAuth(authConfig.Auth)
			if err != nil {
				return &configFile, err
			}
			authConfig.Auth = ""
			configFile.Configs[k] = authConfig
			authConfig.ServerAddress = k
		}
	}
	return &configFile, nil
}

// this method matches a auth configuration to a server address or a url
func (config *ConfigFile) ResolveAuthConfig(registry string) AuthConfig {
	if registry == IndexServerAddress() || len(registry) == 0 {
		// default to the index server
		return config.Configs[IndexServerAddress()]
	}
	// if it's not the index server there are three cases:
	//
	// 1. a full config url -> it should be used as is
	// 2. a full url, but with the wrong protocol
	// 3. a hostname, with an optional port
	//
	// as there is only one auth entry which is fully qualified we need to start
	// parsing and matching

	swapProtocol := func(url string) string {
		if strings.HasPrefix(url, "http:") {
			return strings.Replace(url, "http:", "https:", 1)
		}
		if strings.HasPrefix(url, "https:") {
			return strings.Replace(url, "https:", "http:", 1)
		}
		return url
	}

	resolveIgnoringProtocol := func(url string) AuthConfig {
		if c, found := config.Configs[url]; found {
			return c
		}
		registrySwappedProtocol := swapProtocol(url)
		// now try to match with the different protocol
		if c, found := config.Configs[registrySwappedProtocol]; found {
			return c
		}
		return AuthConfig{}
	}

	// match both protocols as it could also be a server name like httpfoo
	if strings.HasPrefix(registry, "http:") || strings.HasPrefix(registry, "https:") {
		return resolveIgnoringProtocol(registry)
	}

	url := "https://" + registry
	if !strings.Contains(registry, "/") {
		url = url + "/v1/"
	}
	return resolveIgnoringProtocol(url)
}
