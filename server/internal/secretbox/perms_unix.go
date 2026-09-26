package secretbox

import (
	"errors"
	"os"
	"runtime"
)

// checkKeyPerms refuses world/group readable master keys on Unix systems.
func checkKeyPerms(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Mode().Perm()&0o077 != 0 {
		return errors.New(path + ": " + ErrInsecureKeyFile.Error())
	}
	return nil
}
