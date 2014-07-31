package runtime

import (
	"io/ioutil"
	"net"
	"net/http"
	"time"
)

func EC2PublicHostname() (string, error) {
	transport := http.Transport{
		Dial: func(network, addr string) (net.Conn, error) {
			return net.DialTimeout(network, addr, time.Duration(1*time.Second))
		},
	}

	client := http.Client{
		Transport: &transport,
	}
	resp, err := client.Get("http://169.254.169.254/latest/meta-data/public-hostname")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}
