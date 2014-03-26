package utils

import (
	"fmt"
	"strings"
	"time"

	"os"
)

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
	if !strings.Contains(img, "/") {
		return "", img, ""
	}
	parts := strings.Split(img, "/")

	if !strings.Contains(parts[1], ":") {
		return parts[0], parts[1], ""
	}

	imageParts := strings.Split(parts[1], ":")
	// registry, repository, tag
	return parts[0], imageParts[0], imageParts[1]
}

func StringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}

func EtcdJoin(elem ...string) string {
	for i, e := range elem {
		if e != "" {
			joined := strings.Join(elem[i:], "/")
			if !strings.HasPrefix(joined, "/") {
				return "/" + joined
			}
			return joined
		}
	}
	return ""
}

func GetEnv(name, defaultValue string) string {
	if os.Getenv(name) == "" {
		return defaultValue
	}
	return os.Getenv(name)
}
