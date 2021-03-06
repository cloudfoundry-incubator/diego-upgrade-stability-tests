package dusts_test

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"time"

	"code.cloudfoundry.org/bbs"
	"code.cloudfoundry.org/bbs/serviceclient"
	"code.cloudfoundry.org/consuladapter/consulrunner"
	"code.cloudfoundry.org/inigo/helpers"
	"code.cloudfoundry.org/inigo/helpers/certauthority"
	"code.cloudfoundry.org/inigo/helpers/portauthority"
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

	componentLogs *os.File

	oldArtifacts, newArtifacts world.BuiltArtifacts
	addresses                  world.ComponentAddresses
	upgrader                   Upgrader

	bbsClient        bbs.InternalClient
	bbsServiceClient serviceclient.ServiceClient
	logger           lager.Logger
	allocator        portauthority.PortAllocator
	certAuthority    certauthority.CertAuthority

	graceTarballChecksum string

	suiteTempDir string
)

func TestDusts(t *testing.T) {
	helpers.RegisterDefaultTimeouts()
	RegisterFailHandler(Fail)
	RunSpecs(t, "Dusts Suite")
}

var _ = BeforeSuite(func() {
	suiteTempDir = world.TempDir("before-suite")

	if version := os.Getenv("DIEGO_VERSION_V0"); version != diegoGAVersion && version != diegoLocketLocalREVersion {
		Fail("DIEGO_VERSION_V0 not set")
	}

	if graceTarballChecksum = os.Getenv("GRACE_TARBALL_CHECKSUM"); graceTarballChecksum == "" {
		Fail("GRACE_TARBALL_CHECKSUM not set")
	}

	oldArtifacts = world.BuiltArtifacts{
		Lifecycles: world.BuiltLifecycles{},
	}

	oldArtifacts.Lifecycles.BuildLifecycles("dockerapplifecycle", suiteTempDir)
	oldArtifacts.Lifecycles.BuildLifecycles("buildpackapplifecycle", suiteTempDir)
	oldArtifacts.Executables = compileTestedExecutablesV0()

	newArtifacts = world.BuiltArtifacts{
		Lifecycles: world.BuiltLifecycles{},
	}

	newArtifacts.Lifecycles.BuildLifecycles("dockerapplifecycle", suiteTempDir)
	newArtifacts.Lifecycles.BuildLifecycles("buildpackapplifecycle", suiteTempDir)
	newArtifacts.Executables = compileTestedExecutablesV1()

	_, dbBaseConnectionString := world.DBInfo()

	// TODO: the hard coded addresses for router and file server prevent running multiple dusts tests at the same time
	addresses = world.ComponentAddresses{
		Garden:              fmt.Sprintf("127.0.0.1:%d", 10000+config.GinkgoConfig.ParallelNode),
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
		Locket:              fmt.Sprintf("127.0.0.1:%d", 26500+config.GinkgoConfig.ParallelNode),
		SQL:                 fmt.Sprintf("%sdiego_%d", dbBaseConnectionString, config.GinkgoConfig.ParallelNode),
	}

	node := GinkgoParallelNode()
	startPort := 2000 * node
	portRange := 5000
	endPort := startPort + portRange

	allocator, err := portauthority.New(startPort, endPort)
	Expect(err).NotTo(HaveOccurred())

	depotDir := world.TempDirWithParent(suiteTempDir, "depotDir")

	certAuthority, err = certauthority.NewCertAuthority(depotDir, "ca")
	Expect(err).NotTo(HaveOccurred())

	componentLogPath := os.Getenv("DUSTS_COMPONENT_LOG_PATH")
	if componentLogPath == "" {
		componentLogPath = fmt.Sprintf("dusts-component-logs.0.0.0.%d.log", time.Now().Unix())
	}
	componentLogs, err = os.Create(componentLogPath)
	Expect(err).NotTo(HaveOccurred())
	fmt.Printf("Writing component logs to %s\n", componentLogPath)

	ComponentMakerV1 = world.MakeComponentMaker(newArtifacts, addresses, allocator, certAuthority)
	ComponentMakerV1.Setup()

	oldGinkgoWriter := GinkgoWriter
	GinkgoWriter = componentLogs
	defer func() {
		GinkgoWriter = oldGinkgoWriter
	}()
	ComponentMakerV1.GrootFSInitStore()
})

var _ = AfterSuite(func() {
	oldGinkgoWriter := GinkgoWriter
	GinkgoWriter = componentLogs
	defer func() {
		GinkgoWriter = oldGinkgoWriter
	}()
	if ComponentMakerV1 != nil {
		ComponentMakerV1.GrootFSDeleteStore()
	}

	Expect(os.RemoveAll(suiteTempDir)).To(Succeed())
	componentLogs.Close()
})

func QuietBeforeEach(f func()) {
	BeforeEach(func() {
		oldGinkgoWriter := GinkgoWriter
		GinkgoWriter = componentLogs
		defer func() {
			GinkgoWriter = oldGinkgoWriter
		}()
		f()
	})
}

func QuietJustBeforeEach(f func()) {
	JustBeforeEach(func() {
		oldGinkgoWriter := GinkgoWriter
		GinkgoWriter = componentLogs
		defer func() {
			GinkgoWriter = oldGinkgoWriter
		}()
		f()
	})
}

func buildWithGopath(binariesPath, gopath, packagePath string, args ...string) string {
	Expect(os.MkdirAll(binariesPath, 0777)).To(Succeed())
	binaryName := filepath.Base(packagePath)
	expectedBinaryPath := path.Join(binariesPath, binaryName)
	cwd, err := os.Getwd()
	Expect(err).To(Succeed())
	if _, err := os.Stat(expectedBinaryPath); os.IsNotExist(err) {
		fmt.Printf("Building %s with Gopath %s \n", packagePath, gopath)
		Expect(os.Chdir(gopath)).To(Succeed())
		binaryPath, err := gexec.BuildIn(gopath, packagePath, args...)
		Expect(err).NotTo(HaveOccurred())
		Expect(os.Rename(binaryPath, path.Join(binariesPath, binaryName))).To(Succeed())
		Expect(os.Chdir(cwd)).To(Succeed())
	}
	return expectedBinaryPath
}

func buildAsModule(binariesPath, modulepath, packagePath string, args ...string) string {
	Expect(os.MkdirAll(binariesPath, 0777)).To(Succeed())
	binaryName := filepath.Base(packagePath)
	expectedBinaryPath := path.Join(binariesPath, binaryName)
	cwd, err := os.Getwd()
	Expect(err).To(Succeed())
	if _, err := os.Stat(expectedBinaryPath); os.IsNotExist(err) {
		fmt.Printf("Building %s as Module \n", packagePath)
		Expect(os.Chdir(modulepath)).To(Succeed())
		binaryPath, err := gexec.Build(packagePath, args...)
		Expect(err).NotTo(HaveOccurred())
		Expect(os.Rename(binaryPath, path.Join(binariesPath, binaryName))).To(Succeed())
		Expect(os.Chdir(cwd)).To(Succeed())
	}
	return expectedBinaryPath
}

func compileTestedExecutablesV1() world.BuiltExecutables {
	binariesPath := "/tmp/v1_binaries"
	builtExecutables := world.BuiltExecutables{}

	builtExecutables["garden"] = buildAsModule(binariesPath, os.Getenv("GARDEN_GOPATH"), "./cmd/gdn", "-race", "-a", "-tags", "daemon")
	builtExecutables["auctioneer"] = buildAsModule(binariesPath, os.Getenv("AUCTIONEER_GOPATH"), "code.cloudfoundry.org/auctioneer/cmd/auctioneer", "-race")
	builtExecutables["rep"] = buildAsModule(binariesPath, os.Getenv("REP_GOPATH"), "code.cloudfoundry.org/rep/cmd/rep", "-race")
	builtExecutables["bbs"] = buildAsModule(binariesPath, os.Getenv("BBS_GOPATH"), "code.cloudfoundry.org/bbs/cmd/bbs", "-race")
	builtExecutables["locket"] = buildAsModule(binariesPath, os.Getenv("LOCKET_GOPATH"), "code.cloudfoundry.org/locket/cmd/locket", "-race")
	builtExecutables["file-server"] = buildAsModule(binariesPath, os.Getenv("FILE_SERVER_GOPATH"), "code.cloudfoundry.org/fileserver/cmd/file-server", "-race")
	builtExecutables["route-emitter"] = buildAsModule(binariesPath, os.Getenv("ROUTE_EMITTER_GOPATH"), "code.cloudfoundry.org/route-emitter/cmd/route-emitter", "-race")

	Expect(os.Setenv("GO111MODULE", "auto")).To(Succeed())
	builtExecutables["router"] = buildWithGopath(binariesPath, os.Getenv("ROUTER_GOPATH"), "code.cloudfoundry.org/gorouter", "-race")
	builtExecutables["routing-api"] = buildAsModule(binariesPath, os.Getenv("ROUTING_API_GOPATH"), "code.cloudfoundry.org/routing-api/cmd/routing-api", "-race")
	Expect(os.Setenv("GO111MODULE", "")).To(Succeed())

	builtExecutables["ssh-proxy"] = buildAsModule(binariesPath, os.Getenv("SSH_PROXY_GOPATH"), "code.cloudfoundry.org/diego-ssh/cmd/ssh-proxy", "-race")

	os.Setenv("CGO_ENABLED", "0")
	builtExecutables["sshd"] = buildAsModule(binariesPath, os.Getenv("SSHD_GOPATH"), "code.cloudfoundry.org/diego-ssh/cmd/sshd", "-a", "-installsuffix", "static")
	os.Unsetenv("CGO_ENABLED")

	return builtExecutables
}

func compileTestedExecutablesV0() world.BuiltExecutables {
	binariesPath := "/tmp/v0_binaries"
	builtExecutables := world.BuiltExecutables{}

	Expect(os.Setenv("GO111MODULE", "auto")).To(Succeed())

	builtExecutables["auctioneer"] = buildWithGopath(binariesPath, os.Getenv("AUCTIONEER_GOPATH_V0"), "code.cloudfoundry.org/auctioneer/cmd/auctioneer", "-race")
	builtExecutables["rep"] = buildWithGopath(binariesPath, os.Getenv("REP_GOPATH_V0"), "code.cloudfoundry.org/rep/cmd/rep", "-race")
	builtExecutables["bbs"] = buildWithGopath(binariesPath, os.Getenv("BBS_GOPATH_V0"), "code.cloudfoundry.org/bbs/cmd/bbs", "-race")
	builtExecutables["route-emitter"] = buildWithGopath(binariesPath, os.Getenv("ROUTE_EMITTER_GOPATH_V0"), "code.cloudfoundry.org/route-emitter/cmd/route-emitter", "-race")
	builtExecutables["ssh-proxy"] = buildWithGopath(binariesPath, os.Getenv("SSH_PROXY_GOPATH_V0"), "code.cloudfoundry.org/diego-ssh/cmd/ssh-proxy", "-race")

	if os.Getenv("DIEGO_VERSION_V0") == diegoLocketLocalREVersion {
		builtExecutables["locket"] = buildWithGopath(binariesPath, os.Getenv("GOPATH_V0"), "code.cloudfoundry.org/locket/cmd/locket", "-race")
	}

	os.Setenv("CGO_ENABLED", "0")
	builtExecutables["sshd"] = buildWithGopath(binariesPath, os.Getenv("SSHD_GOPATH_V0"), "code.cloudfoundry.org/diego-ssh/cmd/sshd", "-a", "-installsuffix", "static")
	os.Unsetenv("CGO_ENABLED")

	Expect(os.Setenv("GO111MODULE", "")).To(Succeed())
	return builtExecutables
}
