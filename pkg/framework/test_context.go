/*
Copyright 2019 The Interconnectedcloud Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package framework

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/onsi/gomega"

	"github.com/onsi/ginkgo/config"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/klog"
)

const (
	defaultHost = "http://127.0.0.1:8080"
)

// TestContextType contains test settings and global state.
type TestContextType struct {
	KubeConfig               string
	KubeContexts             contextNames
	CertDir                  string
	Host                     string
	RepoRoot                 string
	KubectlPath              string
	OutputDir                string
	ReportDir                string
	ReportPrefix             string
	Prefix                   string
	DeleteNamespace          bool
	DeleteNamespaceOnFailure bool
	CleanStart               bool
}

// TestContext should be used by all tests to access validation context data.
var TestContext TestContextType

// Type to hold array of contexts
type contextNames []string

// String returns a string representing the contextNames
func (c *contextNames) String() string {
	var rep, sep string
	for _, name := range *c {
		rep += sep + name
		sep = ", "
	}
	return rep
}

// Set is used to define the values for custom flag contextNames
func (c *contextNames) Set(v string) error {
	*c = append(*c, v)
	return nil
}

// RegisterFlags registers flags for e2e test suites.
func RegisterFlags() {
	// Turn on verbose by default to get spec names
	config.DefaultReporterConfig.Verbose = true

	// Turn on EmitSpecProgress to get spec progress (especially on interrupt)
	config.GinkgoConfig.EmitSpecProgress = true

	// Randomize specs as well as suites
	config.GinkgoConfig.RandomizeAllSpecs = true

	TestContext.KubeContexts = contextNames{}
	flag.Var(&TestContext.KubeContexts, clientcmd.FlagContext, "kubeconfig context to use/override. If unset, will use value from 'current-context'. Multiple contexts can be provided by specifying it multiple times")
	flag.BoolVar(&TestContext.DeleteNamespace, "delete-namespace", true, "If true tests will delete namespace after completion. It is only designed to make debugging easier, DO NOT turn it off by default.")
	flag.BoolVar(&TestContext.DeleteNamespaceOnFailure, "delete-namespace-on-failure", true, "If true, framework will delete test namespace on failure. Used only during test debugging.")
	flag.StringVar(&TestContext.Host, "host", "", fmt.Sprintf("The host, or apiserver, to connect to. Will default to %s if this argument and --kubeconfig are not set", defaultHost))
	flag.StringVar(&TestContext.ReportPrefix, "report-prefix", "", "Optional prefix for JUnit XML reports. Default is empty, which doesn't prepend anything to the default name.")
	flag.StringVar(&TestContext.ReportDir, "report-dir", "", "Path to the directory where the JUnit XML reports should be saved. Default is empty, which doesn't generate these reports.")
	// kubeconfig flag -> kubeconfig variable -> default location
	//flag.StringVar(&TestContext.KubeConfig, clientcmd.RecommendedConfigPathFlag, os.Getenv(clientcmd.RecommendedConfigPathEnvVar), "Path to kubeconfig containing embedded authinfo.")
	TestContext.KubeConfig = flag.Lookup("kubeconfig").Value.String()
	flag.StringVar(&TestContext.CertDir, "cert-dir", "", "Path to the directory containing the certs. Default is empty, which doesn't use certs.")
	flag.StringVar(&TestContext.RepoRoot, "repo-root", "../../", "Root directory of kubernetes repository, for finding test files.")
	flag.StringVar(&TestContext.KubectlPath, "kubectl-path", "kubectl", "The kubectl binary to use. For development, you might use 'cluster/kubectl.sh' here.")
	flag.StringVar(&TestContext.OutputDir, "e2e-output-dir", "/tmp", "Output directory for interesting/useful test data, like performance data, benchmarks, and other metrics.")
	flag.StringVar(&TestContext.Prefix, "prefix", "e2e", "A prefix to be added to cloud resources created during testing.")
	flag.BoolVar(&TestContext.CleanStart, "clean-start", false, "If true, purge all namespaces except default and system before running tests. This serves to Cleanup test namespaces from failed/interrupted e2e runs in a long-lived cluster.")
}

// HandleFlags sets up all flags and parses the command line.
func HandleFlags() {
	RegisterFlags()
	flag.Parse()
}

func createKubeConfig(clientCfg *restclient.Config) *clientcmdapi.Config {
	clusterNick := "cluster"
	userNick := "user"
	contextNick := "context"

	k8sConfig := clientcmdapi.NewConfig()

	credentials := clientcmdapi.NewAuthInfo()
	credentials.Token = clientCfg.BearerToken
	credentials.ClientCertificate = clientCfg.TLSClientConfig.CertFile
	if len(credentials.ClientCertificate) == 0 {
		credentials.ClientCertificateData = clientCfg.TLSClientConfig.CertData
	}
	credentials.ClientKey = clientCfg.TLSClientConfig.KeyFile
	if len(credentials.ClientKey) == 0 {
		credentials.ClientKeyData = clientCfg.TLSClientConfig.KeyData
	}
	k8sConfig.AuthInfos[userNick] = credentials

	cluster := clientcmdapi.NewCluster()
	cluster.Server = clientCfg.Host
	cluster.CertificateAuthority = clientCfg.CAFile
	if len(cluster.CertificateAuthority) == 0 {
		cluster.CertificateAuthorityData = clientCfg.CAData
	}
	cluster.InsecureSkipTLSVerify = clientCfg.Insecure
	k8sConfig.Clusters[clusterNick] = cluster

	context := clientcmdapi.NewContext()
	context.Cluster = clusterNick
	context.AuthInfo = userNick
	k8sConfig.Contexts[contextNick] = context
	k8sConfig.CurrentContext = contextNick

	return k8sConfig
}

// AfterReadingAllFlags makes changes to the context after all flags
// have been read.
func AfterReadingAllFlags(t *TestContextType) {
	// Only set a default host if one won't be supplied via kubeconfig
	if len(t.Host) == 0 && len(t.KubeConfig) == 0 {
		// Check if we can use the in-cluster config
		if clusterConfig, err := restclient.InClusterConfig(); err == nil {
			if tempFile, err := ioutil.TempFile(os.TempDir(), "kubeconfig-"); err == nil {
				kubeConfig := createKubeConfig(clusterConfig)
				err = clientcmd.WriteToFile(*kubeConfig, tempFile.Name())
				//gomega.Expect(err).To(gomega.BeNil())
				t.KubeConfig = tempFile.Name()
				klog.Infof("Using a temporary kubeconfig file from in-cluster config : %s", tempFile.Name())
			}
		}
		if len(t.KubeConfig) == 0 {
			// This will never get executed since if KubeConfig is empty, GetContexts() would fail.
			klog.Warningf("Unable to find in-cluster config, using default host : %s", defaultHost)
			t.Host = defaultHost
		}
	}
}

// GetContexts returns a list of contexts from provided flags or the current-context if none.
// If KubeConfig not provided or not generated, it returns nil.
func (t TestContextType) GetContexts() []string {
	if len(t.KubeContexts) > 0 {
		return t.KubeContexts
	}

	if len(t.KubeConfig) == 0 {
		t.KubeConfig = os.Getenv(clientcmd.RecommendedConfigPathEnvVar)
	}

	kubeConfig, err := clientcmd.LoadFromFile(t.KubeConfig)
	gomega.Expect(err).To(gomega.BeNil())

	if kubeConfig.CurrentContext == "" {
		panic(fmt.Sprintf("No context provided and current-context not defined in KUBECONFIG file: %s", t.KubeConfig))
	}
	return []string{kubeConfig.CurrentContext}

}

// ContextsAvailable returns the number of contexts available after
// parsing command line arguments.
func (t TestContextType) ContextsAvailable() int {
	return len(t.GetContexts())
}
