package dusts_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"

	"code.cloudfoundry.org/bbs"
	"code.cloudfoundry.org/bbs/serviceclient"
	"code.cloudfoundry.org/consuladapter/consulrunner"
	"code.cloudfoundry.org/inigo/helpers"
	"code.cloudfoundry.org/inigo/world"
	"code.cloudfoundry.org/lager"
	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/config"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"

	"testing"
)

const (
	diegoGAVersion            = "v1.0.0"
	diegoLocketLocalREVersion = "v1.25.2"
)

var (
	ComponentMakerV0, ComponentMakerV1 world.ComponentMaker

	upgrader Upgrader

	bbsClient        bbs.InternalClient
	bbsServiceClient serviceclient.ServiceClient
	logger           lager.Logger
)

func TestDusts(t *testing.T) {
	helpers.RegisterDefaultTimeouts()
	RegisterFailHandler(Fail)
	RunSpecs(t, "Dusts Suite")
}

var _ = SynchronizedBeforeSuite(func() []byte {
	artifacts := make(map[string]world.BuiltArtifacts)
	oldArtifacts := world.BuiltArtifacts{
		Lifecycles: world.BuiltLifecycles{},
	}

	oldArtifacts.Lifecycles.BuildLifecycles("dockerapplifecycle")
	oldArtifacts.Lifecycles.BuildLifecycles("buildpackapplifecycle")
	oldArtifacts.Executables = compileTestedExecutablesV0()
	artifacts["old"] = oldArtifacts

	newArtifacts := world.BuiltArtifacts{
		Lifecycles: world.BuiltLifecycles{},
	}

	newArtifacts.Lifecycles.BuildLifecycles("dockerapplifecycle")
	newArtifacts.Lifecycles.BuildLifecycles("buildpackapplifecycle")
	newArtifacts.Executables = compileTestedExecutablesV1()
	artifacts["new"] = newArtifacts

	payload, err := json.Marshal(artifacts)
	Expect(err).NotTo(HaveOccurred())

	return payload
}, func(payload []byte) {
	var artifacts map[string]world.BuiltArtifacts
	diegoV0Version := os.Getenv("DIEGO_VERSION_V0")

	err := json.Unmarshal(payload, &artifacts)
	Expect(err).NotTo(HaveOccurred())

	_, dbBaseConnectionString := world.DBInfo()

	// TODO: the hard coded addresses for router and file server prevent running multiple dusts tests at the same time
	addresses := world.ComponentAddresses{
		GardenLinux:         fmt.Sprintf("127.0.0.1:%d", 10000+config.GinkgoConfig.ParallelNode),
		NATS:                fmt.Sprintf("127.0.0.1:%d", 11000+config.GinkgoConfig.ParallelNode),
		Consul:              fmt.Sprintf("127.0.0.1:%d", 12750+config.GinkgoConfig.ParallelNode*consulrunner.PortOffsetLength),
		Rep:                 fmt.Sprintf("127.0.0.1:%d", 14000+config.GinkgoConfig.ParallelNode),
		FileServer:          fmt.Sprintf("127.0.0.1:%d", 8080),
		Router:              fmt.Sprintf("127.0.0.1:%d", 80),
		BBS:                 fmt.Sprintf("127.0.0.1:%d", 20500+config.GinkgoConfig.ParallelNode*2),
		Health:              fmt.Sprintf("127.0.0.1:%d", 20500+config.GinkgoConfig.ParallelNode*2+1),
		Auctioneer:          fmt.Sprintf("127.0.0.1:%d", 23000+config.GinkgoConfig.ParallelNode),
		SSHProxy:            fmt.Sprintf("127.0.0.1:%d", 23500+config.GinkgoConfig.ParallelNode),
		SSHProxyHealthCheck: fmt.Sprintf("127.0.0.1:%d", 24500+config.GinkgoConfig.ParallelNode),
		FakeVolmanDriver:    fmt.Sprintf("127.0.0.1:%d", 25500+config.GinkgoConfig.ParallelNode),
		LocalNodePlugin:     fmt.Sprintf("127.0.0.1:%d", 25550+config.GinkgoConfig.ParallelNode),
		Locket:              fmt.Sprintf("127.0.0.1:%d", 26500+config.GinkgoConfig.ParallelNode),
		SQL:                 fmt.Sprintf("%sdiego_%d", dbBaseConnectionString, config.GinkgoConfig.ParallelNode),
	}

	switch diegoV0Version {
	case diegoGAVersion:
		ComponentMakerV0 = world.MakeV0ComponentMaker("fixtures/certs/", artifacts["old"], addresses)
		upgrader = NewGAUpgrader()
	case diegoLocketLocalREVersion:
		ComponentMakerV0 = world.MakeComponentMaker("fixtures/certs/", artifacts["old"], addresses)
		upgrader = NewLocketLocalREUpgrader()
	default:
		Expect(diegoV0Version).To(Or(Equal(diegoGAVersion), Equal(diegoLocketLocalREVersion)))
	}

	ComponentMakerV1 = world.MakeComponentMaker("fixtures/certs/", artifacts["new"], addresses)

	ComponentMakerV1.GrootFSInitStore()
})

var _ = AfterSuite(func() {
	ComponentMakerV1.GrootFSDeleteStore()
})

func lazyBuild(binariesPath, gopath, packagePath string, args ...string) string {
	Expect(os.MkdirAll(binariesPath, 0777)).To(Succeed())
	binaryName := filepath.Base(packagePath)
	expectedBinaryPath := path.Join(binariesPath, binaryName)
	if _, err := os.Stat(expectedBinaryPath); os.IsNotExist(err) {
		fmt.Printf("Building %s\n", packagePath)
		binaryPath, err := gexec.BuildIn(gopath, packagePath, args...)
		Expect(err).NotTo(HaveOccurred())
		Expect(os.Rename(binaryPath, path.Join(binariesPath, binaryName))).To(Succeed())
	}
	return expectedBinaryPath
}

func compileTestedExecutablesV1() world.BuiltExecutables {
	fmt.Println("Lazily building V1 executables")
	binariesPath := "/tmp/v1_binaries"
	builtExecutables := world.BuiltExecutables{}

	builtExecutables["garden"] = lazyBuild(binariesPath, os.Getenv("GARDEN_GOPATH"), "code.cloudfoundry.org/guardian/cmd/gdn", "-race", "-a", "-tags", "daemon")
	builtExecutables["auctioneer"] = lazyBuild(binariesPath, os.Getenv("AUCTIONEER_GOPATH"), "code.cloudfoundry.org/auctioneer/cmd/auctioneer", "-race")
	builtExecutables["rep"] = lazyBuild(binariesPath, os.Getenv("REP_GOPATH"), "code.cloudfoundry.org/rep/cmd/rep", "-race")
	builtExecutables["bbs"] = lazyBuild(binariesPath, os.Getenv("BBS_GOPATH"), "code.cloudfoundry.org/bbs/cmd/bbs", "-race")
	builtExecutables["locket"] = lazyBuild(binariesPath, os.Getenv("LOCKET_GOPATH"), "code.cloudfoundry.org/locket/cmd/locket", "-race")
	builtExecutables["file-server"] = lazyBuild(binariesPath, os.Getenv("FILE_SERVER_GOPATH"), "code.cloudfoundry.org/fileserver/cmd/file-server", "-race")
	builtExecutables["route-emitter"] = lazyBuild(binariesPath, os.Getenv("ROUTE_EMITTER_GOPATH"), "code.cloudfoundry.org/route-emitter/cmd/route-emitter", "-race")
	builtExecutables["router"] = lazyBuild(binariesPath, os.Getenv("ROUTER_GOPATH"), "code.cloudfoundry.org/gorouter", "-race")
	builtExecutables["routing-api"] = lazyBuild(binariesPath, os.Getenv("ROUTING_API_GOPATH"), "code.cloudfoundry.org/routing-api/cmd/routing-api", "-race")
	builtExecutables["ssh-proxy"] = lazyBuild(binariesPath, os.Getenv("SSH_PROXY_GOPATH"), "code.cloudfoundry.org/diego-ssh/cmd/ssh-proxy", "-race")

	os.Setenv("CGO_ENABLED", "0")
	builtExecutables["sshd"] = lazyBuild(binariesPath, os.Getenv("SSHD_GOPATH"), "code.cloudfoundry.org/diego-ssh/cmd/sshd", "-a", "-installsuffix", "static")
	os.Unsetenv("CGO_ENABLED")

	return builtExecutables
}

func compileTestedExecutablesV0() world.BuiltExecutables {
	fmt.Println("Lazily building V0 executables")
	binariesPath := "/tmp/v0_binaries"
	builtExecutables := world.BuiltExecutables{}

	builtExecutables["auctioneer"] = lazyBuild(binariesPath, os.Getenv("AUCTIONEER_GOPATH_V0"), "code.cloudfoundry.org/auctioneer/cmd/auctioneer", "-race")
	builtExecutables["rep"] = lazyBuild(binariesPath, os.Getenv("REP_GOPATH_V0"), "code.cloudfoundry.org/rep/cmd/rep", "-race")
	builtExecutables["bbs"] = lazyBuild(binariesPath, os.Getenv("BBS_GOPATH_V0"), "code.cloudfoundry.org/bbs/cmd/bbs", "-race")
	builtExecutables["route-emitter"] = lazyBuild(binariesPath, os.Getenv("ROUTE_EMITTER_GOPATH_V0"), "code.cloudfoundry.org/route-emitter/cmd/route-emitter", "-race")
	builtExecutables["ssh-proxy"] = lazyBuild(binariesPath, os.Getenv("SSH_PROXY_GOPATH_V0"), "code.cloudfoundry.org/diego-ssh/cmd/ssh-proxy", "-race")

	if os.Getenv("DIEGO_VERSION_V0") == "v1.25.2" {
		builtExecutables["locket"] = lazyBuild(binariesPath, os.Getenv("GOPATH_V0"), "code.cloudfoundry.org/locket/cmd/locket", "-race")
	}

	os.Setenv("CGO_ENABLED", "0")
	builtExecutables["sshd"] = lazyBuild(binariesPath, os.Getenv("SSHD_GOPATH_V0"), "code.cloudfoundry.org/diego-ssh/cmd/sshd", "-a", "-installsuffix", "static")
	os.Unsetenv("CGO_ENABLED")

	return builtExecutables
}
