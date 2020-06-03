/**
  Load balancer artifact
 */
package artifacts

import (
	"github.com/golang/glog"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
)

/**
   Configuration section for load balancer
 */
type Server struct {
	Address 	string 				`yaml:"address"`
	Port        string 				`yaml:"port"`
}

type Api struct {
	Address 	string 				`yaml:"address"`
	Rest        string 				`yaml:"rest"`
	Grpc        string 				`yaml:"grpc"`
}

type Pool struct {
	Name        string       		`yaml:"name"`
	Api 		[]Api				`yaml:"api"`
	Servers 	[]Server 			`yaml:"servers"`
}

type Artifact struct {
	Service struct {
		Pool           Pool  	`yaml:"pool"`
	}  `yaml:"artifact"`
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
		glog.Info("Reading config ", pwd + "/" + file)
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
	} else {
		artifact.BaseDir = filepath.Base(file)
	}

	glog.Infof("Base dir [%s]", dir)

	//TODO validation
	return artifact, nil
}
