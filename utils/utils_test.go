package utils

import (
	"testing"
)

func TestSplitDockerImageRepository(t *testing.T) {
	registry, repository, tag := SplitDockerImage("ubuntu")

	if registry != "" {
		t.Fail()
	}
	if repository != "ubuntu" {
		t.Fail()
	}
	if tag != "" {
		t.Fail()
	}
}

func TestSplitDockerImageWithRegistry(t *testing.T) {
	registry, repository, tag := SplitDockerImage("custom.registry/ubuntu")

	if registry != "custom.registry" {
		t.Fail()
	}
	if repository != "ubuntu" {
		t.Fail()
	}
	if tag != "" {
		t.Fail()
	}
}

func TestSplitDockerImageWithRegistryAndTag(t *testing.T) {
	registry, repository, tag := SplitDockerImage("custom.registry/ubuntu:12.04")

	if registry != "custom.registry" {
		t.Fail()
	}
	if repository != "ubuntu" {
		t.Fail()
	}
	if tag != "12.04" {
		t.Fail()
	}
}

func TestSplitDockerImageWithRepositoryAndTag(t *testing.T) {
	registry, repository, tag := SplitDockerImage("ubuntu:12.04")

	if registry != "" {
		t.Fail()
	}

	if repository != "ubuntu" {
		t.Fail()
	}

	if tag != "12.04" {
		t.Fail()
	}
}
