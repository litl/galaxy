package utils

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/codegangsta/cli"

	"os"
)

type SliceVar []string

func (s *SliceVar) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func (s *SliceVar) String() string {
	return strings.Join(*s, ",")
}

type OutputBuffer struct {
	Output []string
}

func (o *OutputBuffer) Log(msg string) {
	o.Output = append(o.Output, msg)
}

// HumanDuration returns a human-readable approximation of a duration
// (eg. "About a minute", "4 hours ago", etc.)
func HumanDuration(d time.Duration) string {
	if seconds := int(d.Seconds()); seconds < 1 {
		return "Less than a second"
	} else if seconds < 60 {
		return fmt.Sprintf("%d seconds", seconds)
	} else if minutes := int(d.Minutes()); minutes == 1 {
		return "About a minute"
	} else if minutes < 60 {
		return fmt.Sprintf("%d minutes", minutes)
	} else if hours := int(d.Hours()); hours == 1 {
		return "About an hour"
	} else if hours < 48 {
		return fmt.Sprintf("%d hours", hours)
	} else if hours < 24*7*2 {
		return fmt.Sprintf("%d days", hours/24)
	} else if hours < 24*30*3 {
		return fmt.Sprintf("%d weeks", hours/24/7)
	} else if hours < 24*365*2 {
		return fmt.Sprintf("%d months", hours/24/30)
	}
	return fmt.Sprintf("%f years", d.Hours()/24/365)
}

func SplitDockerImage(img string) (string, string, string) {
	index := 0
	repository := img
	var registry, tag string
	if strings.Contains(img, "/") {
		separator := strings.Index(img, "/")
		registry = img[index:separator]
		index = separator + 1
		repository = img[index:]
	}

	if strings.Contains(img, ":") {
		separator := strings.Index(img, ":")
		repository = img[index:separator]
		index = separator + 1
		tag = img[index:]
	}

	return registry, repository, tag
}

func StringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}

func RemoveStringInSlice(a string, list []string) []string {
	r := []string{}
	for _, v := range list {
		if v != a {
			r = append(r, v)
		}
	}
	return r
}

func GetEnv(name, defaultValue string) string {
	if os.Getenv(name) == "" {
		return defaultValue
	}
	return os.Getenv(name)
}

func HomeDir() string {
	return os.Getenv("HOME")
}

func GalaxyEnv(c *cli.Context) string {
	if c.GlobalString("env") != "" {
		return strings.TrimSpace(c.GlobalString("env"))
	}
	return strings.TrimSpace(GetEnv("GALAXY_ENV", ""))
}

func GalaxyPool(c *cli.Context) string {
	if c.GlobalString("pool") != "" {
		return strings.TrimSpace(c.GlobalString("pool"))
	}
	return strings.TrimSpace(GetEnv("GALAXY_POOL", ""))
}

func GalaxyRedisHost(c *cli.Context) string {
	if c.GlobalString("registry") != "" {
		return strings.TrimSpace(c.GlobalString("registry"))
	}

	return strings.TrimSpace(GetEnv("GALAXY_REGISTRY_URL", ""))
}

// NextSlot finds the first available index in an array of integers
func NextSlot(used []int) int {
	free := 0
RESTART:
	for _, v := range used {
		if v == free {
			free = free + 1
			goto RESTART
		}
	}
	return free
}

func ParseMemory(mem string) (int64, error) {
	if mem == "" {
		return 0, nil
	}

	multiplier := int64(1)
	if strings.HasSuffix(mem, "b") {
		mem = mem[:len(mem)-1]
	}

	if strings.HasSuffix(mem, "k") {
		multiplier = int64(1024)
		mem = mem[:len(mem)-1]
	}

	if strings.HasSuffix(mem, "m") {
		multiplier = int64(1024) * int64(1024)
		mem = mem[:len(mem)-1]
	}

	if strings.HasSuffix(mem, "g") {
		multiplier = int64(1024) * int64(1024) * int64(1024)
		mem = mem[:len(mem)-1]
	}

	i, err := strconv.ParseInt(mem, 10, 64)
	if err != nil {
		return 0, err
	}
	return i * multiplier, nil
}
