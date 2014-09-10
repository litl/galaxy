package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
)

// marshal whatever we've got with out default indentation
// swallowing errors.
func marshal(i interface{}) []byte {
	jsonBytes, err := json.MarshalIndent(i, "", "  ")
	if err != nil {
		log.Println("error encoding json:", err)
	}
	return append(jsonBytes, '\n')
}

// random 64bit ID
func genId() string {
	b := make([]byte, 8)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}
