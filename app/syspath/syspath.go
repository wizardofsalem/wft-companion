package syspath

import "os"

func System32(name string) string {
	return system32(os.Getenv, name)
}

func system32(getenv func(string) string, name string) string {
	root := getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	return root + `\System32\` + name
}
