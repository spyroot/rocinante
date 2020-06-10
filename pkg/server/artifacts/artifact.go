package artifacts

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"

	"github.com/golang/glog"
	"github.com/spyroot/rocinante/pkg/io"
	"gopkg.in/yaml.v2"
)

type Controller struct {
	Address string `yaml:"address"`
	Port    string `yaml:"port"`
	Rest    string `yaml:"rest"`
	WWWRoot string `yaml:"wwwroot"`
}

type Cluster struct {
	Name        string       `yaml:"name"`
	Controllers []Controller `yaml:"controllers"`
}

type Artifact struct {
	Formation struct {
		Cluster Cluster `yaml:"cluster"`
	} `yaml:"artifact"`
	BaseDir string
}

/*
   Return true if file exists
*/
func fileExists(filename string) bool {
	stat, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}
	return !stat.IsDir()
}

/**
  Reads config.yml file and serialize everything in JetConfig struct.
*/
func Read(file string) (Artifact, error) {

	var artifact Artifact
	var base string

	if !fileExists(file) {
		pwd, err := os.Getwd()
		if err != nil {
			return artifact, err
		}
		glog.Info("Reading config ", pwd+"/"+file)
		base = filepath.Join(pwd, file)
	}

	// if file exist check if location current dir
	dir := filepath.Dir(file)
	if dir == "." {
		pwd, err := os.Getwd()
		if err != nil {
			return artifact, err
		}
		base = filepath.Join(pwd, file)
	}

	base = file
	glog.Info("Reading config ", file)

	data, err := ioutil.ReadFile(base)
	if err != nil {
		return artifact, err
	}

	log.Println("Parsing server artifact file.")
	err = yaml.Unmarshal(data, &artifact)
	if err != nil {
		return artifact, err
	}

	if dir == "." {
		pwd, _ := os.Getwd()
		dir, err = filepath.Abs(pwd)
		artifact.BaseDir = dir
		glog.Infof("Setting current dir as base %s", artifact.BaseDir)
	} else {
		glog.Infof("setting base dir %s", filepath.Dir(file))
		artifact.BaseDir = filepath.Dir(file)
	}

	ok, err := io.IsDir(artifact.BaseDir)
	if err != nil {
		return artifact, fmt.Errorf(err.Error())
	}
	if ok == false {
		return artifact, fmt.Errorf("can't determien a base dir")
	}

	glog.Infof("Base dir [%s]", dir)

	//TODO validation
	return artifact, nil
}
