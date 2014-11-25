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

func TestParseMemBlank(t *testing.T) {
	i, err := ParseMemory("")
	if err != nil {
		t.Fatalf("Expected 0. Got %s", err)
	}

	if i != 0 {
		t.Fatal("Expected 0")
	}
}

func TestParseMemNaN(t *testing.T) {
	_, err := ParseMemory("abc")
	if err == nil {
		t.Fatalf("Expected error. Got %s", nil)
	}
}

func TestParseMemBasic(t *testing.T) {
	i, err := ParseMemory("1024")
	if err != nil {
		t.Fatalf("Expected 1024. Got %s", err)
	}
	if i != 1024 {
		t.Fatal("Expected 1024")
	}
}

func TestParseMemByteSuffix(t *testing.T) {
	i, err := ParseMemory("2048b")
	if err != nil {
		t.Fatalf("Expected 2048. Got %s", err)
	}
	if i != 2048 {
		t.Fatal("Expected 2048")
	}
}

func TestParseMemKiloSuffix(t *testing.T) {
	i, err := ParseMemory("2k")
	if err != nil {
		t.Fatalf("Expected 2048. Got %s", err)
	}
	if i != 2048 {
		t.Fatal("Expected 2048")
	}
}

func TestParseMemMegSuffix(t *testing.T) {
	i, err := ParseMemory("3m")
	if err != nil {
		t.Fatalf("Expected 3145728. Got %s", err)
	}
	if i != 3145728 {
		t.Fatal("Expected 3145728")
	}
}

func TestParseMemGigSuffix(t *testing.T) {
	i, err := ParseMemory("4g")
	if err != nil {
		t.Fatalf("Expected 4294967296. Got %s", err)
	}
	if i != 4294967296 {
		t.Fatal("Expected 4294967296")
	}
}
