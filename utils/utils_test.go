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

func TestSplitDockerImageWithPublicRegistry(t *testing.T) {
	registry, repository, tag := SplitDockerImage("username/ubuntu")

	if registry != "username" {
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

func TestNextSlotEmpty(t *testing.T) {
	if NextSlot([]int{}) != 0 {
		t.Fatal("Expected 0")
	}
}

func TestNextSlotSimple(t *testing.T) {
	if NextSlot([]int{0}) != 1 {
		t.Fatal("Expected 1")
	}
}

func TestNextSlotGap(t *testing.T) {
	if NextSlot([]int{0, 1, 3}) != 2 {
		t.Fatal("Expected 2")
	}
}

func TestNextSlotEnd(t *testing.T) {
	if NextSlot([]int{0, 1, 2, 3}) != 4 {
		t.Fatal("Expected 4")
	}
}
