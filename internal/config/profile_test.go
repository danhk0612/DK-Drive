package config

import "testing"

func TestProfileValidate(t *testing.T) {
	profile := Profile{
		Name:        "Test SFTP",
		Protocol:    ProtocolSFTP,
		DriveLetter: "M",
		Host:        "example.test",
		Port:        22,
		Username:    "tester",
		AuthMethod:  AuthPassword,
	}

	if err := profile.Validate(); err != nil {
		t.Fatalf("Validate(): %v", err)
	}
}

func TestProfileValidateRejectsMissingPrivateKey(t *testing.T) {
	profile := Profile{
		Name:        "Test SFTP",
		Protocol:    ProtocolSFTP,
		DriveLetter: "M",
		Host:        "example.test",
		Port:        22,
		Username:    "tester",
		AuthMethod:  AuthPrivateKey,
	}

	if err := profile.Validate(); err == nil {
		t.Fatal("Validate() returned nil error")
	}
}
