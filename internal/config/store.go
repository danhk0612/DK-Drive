package config

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Secrets never belongs in Profile or plaintext settings JSON.
type Secrets struct {
	Password   string
	Passphrase string
}

type SavedProfile struct {
	ID              string
	Profile         Profile
	ProtectedSecret []byte `json:",omitempty"`
}

type Settings struct {
	Version     int
	CloseToTray bool
	Profiles    []SavedProfile
}

func DefaultSettings() Settings { return Settings{Version: 1, CloseToTray: true} }

func NewID() (string, error) {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(data[:]), nil
}

func (s Settings) Validate() error {
	if s.Version != 1 {
		return errors.New("지원하지 않는 설정 버전입니다; 기존 파일을 유지합니다")
	}
	ids, drives := map[string]bool{}, map[string]bool{}
	for _, p := range s.Profiles {
		if p.ID == "" || ids[p.ID] {
			return errors.New("연결 ID가 없거나 중복되었습니다")
		}
		if err := p.Profile.Validate(); err != nil {
			return fmt.Errorf("연결 설정 오류: %w", err)
		}
		drive := strings.ToUpper(p.Profile.DriveLetter)
		if drives[drive] {
			return fmt.Errorf("드라이브 %s: 설정이 중복되었습니다", drive)
		}
		ids[p.ID], drives[drive] = true, true
	}
	return nil
}

type Protector interface {
	Protect([]byte) ([]byte, error)
	Unprotect([]byte) ([]byte, error)
}

func SealSecrets(p Protector, s Secrets) ([]byte, error) {
	data, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	defer clear(data)
	return p.Protect(data)
}

func OpenSecrets(p Protector, data []byte) (Secrets, error) {
	if len(data) == 0 {
		return Secrets{}, nil
	}
	plain, err := p.Unprotect(data)
	if err != nil {
		return Secrets{}, errors.New("저장된 자격 증명을 복호화할 수 없습니다; 원래 Windows 사용자/PC인지 확인하세요")
	}
	defer clear(plain)
	var s Secrets
	if err := json.Unmarshal(plain, &s); err != nil {
		return Secrets{}, errors.New("저장된 자격 증명 형식이 올바르지 않습니다")
	}
	if strings.ContainsRune(s.Password, 0) || strings.ContainsRune(s.Passphrase, 0) {
		return Secrets{}, errors.New("저장된 자격 증명에 지원하지 않는 NUL 문자가 있습니다")
	}
	return s, nil
}

func SettingsPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "DKDrive", "settings.json"), nil
}

func LoadSettings(filename string) (Settings, error) {
	data, err := os.ReadFile(filename)
	if errors.Is(err, os.ErrNotExist) {
		return DefaultSettings(), nil
	}
	if err != nil {
		return Settings{}, err
	}
	var s Settings
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&s); err != nil {
		return Settings{}, errors.New("설정 파일을 읽을 수 없습니다; 원본을 덮어쓰지 않습니다")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return Settings{}, errors.New("설정 파일 뒤에 불필요한 데이터가 있습니다")
	}
	return s, s.Validate()
}

// SaveSettings replaces only after the complete new document has been synced.
// No remove-then-rename fallback: replacement failure preserves the original.
func SaveSettings(filename string, s Settings) error {
	if err := s.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(filename)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".settings-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, writeErr := tmp.Write(append(data, '\n'))
	syncErr := tmp.Sync()
	closeErr := tmp.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filename)
}
