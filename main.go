package main

import (
	b64 "encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/knadh/koanf/parsers/dotenv"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var conf = koanf.Conf{
	Delim:       "_",
	StrictMerge: true,
}
var k = koanf.NewWithConf(conf)

const (
	prefix = "DDNS_"
	listener_host = "0.0.0.0"
	listener_port = "9376"
	listener_type = "tcp"
)

func main() {
	var envFile string
	// load env file if a path is provided. otherwise, we'll get params from environment.
	flag.StringVar(&envFile, "envfile", "", "Path to .env file containing parameters to load`")
	flag.Parse()

	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	zerolog.SetGlobalLevel(zerolog.InfoLevel)

	conf := loadConfig(envFile)

	if conf.Debug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}

	createFileStorage(conf.DBPath)

	go MonitorIPAddress(conf)

	// Listen for incoming connections.
    l, err := net.Listen(listener_type, listener_host+":"+listener_port)
    if err != nil {
        fmt.Println("Error listening:", err.Error())
        os.Exit(1)
    }

    // Close the listener when the application closes.
    defer l.Close()
    fmt.Println("Listening for health check on " + listener_host + ":" + listener_port)
    for {
        // Listen for an incoming connection.
        _, err := l.Accept()
        if err != nil {
            fmt.Println("Error accepting: ", err.Error())
            os.Exit(1)
        }
        // Handle connections in a new goroutine.
        go handleHealthCheck()
    }

	log.Info().Msg("Shutting down")
}

func handleHealthCheck() {
	//log.Debug().Msg("Health check")
}

func loadConfig(envFile string) Config {
	// if running locally, provide path to local env file to use for testing.
	if envFile != "" {
		if err := k.Load(file.Provider(envFile), dotenv.ParserEnv(prefix, ".", func(k string) string {
			return strings.Replace(strings.ToLower(strings.TrimPrefix(k, prefix)), "_", ".", -1)
		})); err != nil {
			log.Fatal().Msgf("unable to load .env file due to error: %s", err.Error())
		}
	} else {
		if err := k.Load(env.Provider(prefix, ".", func(k string) string {
			return strings.Replace(strings.ToLower(strings.TrimPrefix(k, prefix)), "_", ".", -1)
		}), nil); err != nil {
			log.Fatal().Msgf("unable to read env vars due to error: %s", err.Error())
		}
	}

	var conf Config

	//unmarshal env vars to config struct
	if err := k.UnmarshalWithConf("", &conf, koanf.UnmarshalConf{Tag: "koanf", FlatPaths: true}); err != nil {
		log.Fatal().Msgf("unable to marshal env vars to struct: %s", err.Error())
	}

	return conf
}

type Config struct {
	BaseHost string        `koanf:"basehost"`
	UpdHost  string        `koanf:"updhost"`
	Username string        `koanf:"un"`
	Password string        `koanf:"pw"`
	DNSHost  string        `koanf:"dnshost"`
	Wildcard string        `koanf:"wildcard"`
	MX       string        `koanf:"mx"`
	BackMX   string        `koanf:"backmx"`
	DBPath   string        `koanf:"dbpath"`
	Interval time.Duration `koanf:"interval"`
	Debug    bool          `koanf:"debug"`
}

func (c *Config) getPublicIPReqURL() string {
	return fmt.Sprintf("http://%s/", c.BaseHost)
}

func (c *Config) getUpdateReqURL(ip string) string {
	return fmt.Sprintf("https://%s/nic/update?hostname=%s&myip=%s&&wildcard=%s&mx=%s&backmx=%s", c.UpdHost, c.DNSHost, ip, c.Wildcard, c.MX, c.BackMX)
}

func MonitorIPAddress(conf Config) {
	ticker := time.NewTicker(conf.Interval)

	log.Info().Msgf("Starting DNS check loop interval: %v", conf.Interval)
	for ; true; <-ticker.C {
		// get public IP address using API
		ip, err := getPublicIP(conf)
		if err != nil {
			log.Error().Str("Error", err.Error()).Msg("unable to get public IP")
			continue
		}
		log.Debug().Msgf("Current IP: %s", ip.To4().String())
		if ip == nil {
			continue
		}

		// get previous IP from local file
		prevIP, err := getPreviousIP(conf.DBPath)
		if err != nil {
			log.Error().Str("Error", err.Error()).Msg("error attempting to read last IP from file")
			return
		}
		log.Debug().Msgf("Previous IP: %v", prevIP)

		if prevIP == nil || prevIP.To4().String() != ip.To4().String() {
			err := updateDNS(conf, ip)
			if err != nil {
				log.Error().Str("Error", err.Error()).Msg("unable to update DNS via API")
			}

			err = persistNewIP(conf.DBPath, ip)
			if err != nil {
				log.Error().Str("Error", err.Error()).Msg("unable to persist public IP")
			}
		} else {
			log.Debug().Msg("no update required")
		}
	}
	log.Debug().Msg("exiting loop")
}

// getPublicIP fetches the current public IP address from the DNS-O-Matic API.
func getPublicIP(conf Config) (net.IP, error) {
	res, err := http.Get(conf.getPublicIPReqURL())
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	log.Debug().Msgf("Status code response from DNS-O-Matic API: %d", res.StatusCode)
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	if res.StatusCode != http.StatusOK {
		return nil, errors.New(fmt.Sprintf("unsuccessful status code returned from API: %d", res.StatusCode))
	}

	ipStr := string(body)
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return nil, errors.New(fmt.Sprintf("unable to parse IP address: %s", ipStr))
	}

	return ip, nil
}

func updateDNS(conf Config, ip net.IP) error {
	log.Debug().Msg("Updating DNS...")
	req, err := http.NewRequest(http.MethodPost, conf.getUpdateReqURL(ip.To4().String()), nil)
	if err != nil {
		return err
	}

	auth := "Basic " + b64.StdEncoding.EncodeToString([]byte(conf.Username+":"+conf.Password))
	req.Header.Set("Authorization", auth)

	client := http.Client{
		Timeout: 30 * time.Second,
	}

	res, err := client.Do(req)
	if err != nil {
		log.Error().Str("StatusCode", res.Status).Msg("unable to update DNS record")
		return err
	}
	log.Debug().Msg("DNS updated")
	return nil
}

func getPreviousIP(path string) (net.IP, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(string(data))
	return ip, err
}

func persistNewIP(path string, ip net.IP) error {
	d := []byte(ip.To4().String())
	return os.WriteFile(path, d, 0666)
}

func createFileStorage(path string) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		f, err := os.Create(path)
		if err != nil {
			log.Fatal().Str("Error", err.Error()).Str("Path", path).Msg("unable to create new db file")
		}
		f.Close()
	}
}
