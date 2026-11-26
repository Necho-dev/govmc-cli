package target

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gofrs/flock"
)

const secretPrefix = "enc:v1:"

func keyPath(configPath string) string { return filepath.Join(filepath.Dir(configPath), "config.key") }

func encryptionKey(configPath string, create bool) ([]byte, error) {
	path := keyPath(configPath)
	read := func() ([]byte, error) {
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return nil, errors.New("credential key must be a regular file accessible only by its owner (mode 0600)")
		}
		if info.Size() != 32 {
			return nil, errors.New("invalid credential key; restore the original config.key")
		}
		key, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if len(key) != 32 {
			return nil, errors.New("invalid credential key; restore the original config.key")
		}
		return key, nil
	}
	key, err := read()
	if err == nil || !errors.Is(err, os.ErrNotExist) {
		return key, err
	}
	if !create {
		return nil, errors.New("credential key missing; restore config.key together with config.yaml")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	lock := flock.New(path + ".lock")
	defer lock.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	locked, err := lock.TryLockContext(ctx, 20*time.Millisecond)
	if err != nil {
		return nil, err
	}
	if !locked {
		return nil, errors.New("could not lock credential key")
	}
	key, err = read()
	if err == nil || !errors.Is(err, os.ErrNotExist) {
		return key, err
	}
	key = make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := atomicPrivateWrite(path, key); err != nil {
		return nil, err
	}
	return key, nil
}

func credentialAAD(name string, p Profile) []byte {
	return []byte("govmc/v1\x00" + name + "\x00" + p.URL + "\x00" + p.Username + "\x00" + strconv.FormatBool(p.Insecure))
}

// SetPassword encrypts a credential for this exact profile and endpoint. Plaintext
// is never part of Profile or Config, and JSON inspection omits the ciphertext.
func SetPassword(configPath, name string, p *Profile, password string) error {
	if password == "" {
		return errors.New("password must not be empty")
	}
	check := *p
	check.Password, check.PasswordEnv = "", ""
	if err := Validate(name, check); err != nil {
		return err
	}
	if _, err := os.Stat(keyPath(configPath)); errors.Is(err, os.ErrNotExist) {
		config, loadErr := Load(configPath)
		if loadErr != nil {
			return loadErr
		}
		for _, existing := range config.Profiles {
			if existing.Password != "" {
				return errors.New("credential key missing; restore config.key before changing saved passwords")
			}
		}
	}
	key, err := encryptionKey(configPath, true)
	if err != nil {
		return err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	aead, err := cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		return err
	}
	p.Password = secretPrefix + base64.RawStdEncoding.EncodeToString(aead.Seal(nil, nil, []byte(password), credentialAAD(name, *p)))
	p.PasswordEnv = ""
	return nil
}

func decryptPassword(configPath, name string, p Profile) (string, error) {
	if !strings.HasPrefix(p.Password, secretPrefix) {
		return "", fmt.Errorf("profile %q has an unsupported password format; use govmc profiles update %s --password-stdin", name, name)
	}
	key, err := encryptionKey(configPath, false)
	if err != nil {
		return "", err
	}
	encrypted, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(p.Password, secretPrefix))
	if err != nil {
		return "", errors.New("invalid encrypted credential")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	aead, err := cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		return "", err
	}
	if len(encrypted) < aead.Overhead() {
		return "", errors.New("invalid encrypted credential")
	}
	plaintext, err := aead.Open(nil, nil, encrypted, credentialAAD(name, p))
	if err != nil {
		return "", fmt.Errorf("cannot decrypt password for profile %q; restore the matching key or save the password again", name)
	}
	return string(plaintext), nil
}

func atomicPrivateWrite(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".govmc-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(data)
	syncErr := f.Sync()
	closeErr := f.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
