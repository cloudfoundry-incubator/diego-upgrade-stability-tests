package dusts_test

import (
	"os"
	"os/exec"
	"path/filepath"

	auctioneerconfig "code.cloudfoundry.org/auctioneer/cmd/auctioneer/config"
	bbsconfig "code.cloudfoundry.org/bbs/cmd/bbs/config"
	"code.cloudfoundry.org/inigo/helpers"
	"code.cloudfoundry.org/inigo/world"
	"code.cloudfoundry.org/lager"
	"code.cloudfoundry.org/localip"
	repconfig "code.cloudfoundry.org/rep/cmd/rep/config"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/ginkgomon"
	"github.com/tedsuo/ifrit/grouper"
)

var (
	repV0UnsupportedVizziniTests = []string{"MaxPids", "CF_INSTANCE_INTERNAL_IP"}
	// security_group_tests in V0 vizzini won't pass since they try to access the
	// router (as opposed to www.example.com in recent versions). Security groups
	// don't affect access to the host machine, therefore they cannot block
	// traffic which causes both tests in that file to fail
	securityGroupV0Tests = "should allow access to an internal IP"
)

var _ = Describe("UpgradeVizzini", func() {
	var (
		plumbing                                             ifrit.Process
		locket, bbs, routeEmitter, sshProxy, auctioneer, rep ifrit.Process
		locketRunner                                         ifrit.Runner
		bbsRunner                                            ifrit.Runner
		routeEmitterRunner                                   ifrit.Runner
		sshProxyRunner                                       ifrit.Runner
		auctioneerRunner                                     ifrit.Runner
		repRunner                                            ifrit.Runner
		bbsClientGoPathEnvVar                                string
	)

	Context("from v1.0.0", func() {
		BeforeEach(func() {
			logger = lager.NewLogger("test")
			logger.RegisterSink(lager.NewWriterSink(GinkgoWriter, lager.DEBUG))

			bbsClientGoPathEnvVar = "GOPATH_V0"

			ComponentMakerV0 = world.MakeV0ComponentMaker("fixtures/certs/", oldArtifacts, addresses)

			fileServer, _ := ComponentMakerV1.FileServer()

			plumbing = ginkgomon.Invoke(grouper.NewParallel(os.Kill, grouper.Members{
				{Name: "nats", Runner: ComponentMakerV1.NATS()},
				{Name: "sql", Runner: ComponentMakerV1.SQL()},
				{Name: "consul", Runner: ComponentMakerV1.Consul()},
				{Name: "file-server", Runner: fileServer},
				// {Name: "garden", Runner: ComponentMakerV1.Garden()},
				{Name: "router", Runner: ComponentMakerV1.Router()},
			}))
			helpers.ConsulWaitUntilReady(ComponentMakerV0.Addresses())

			bbsRunner = ComponentMakerV0.BBS()
			routeEmitterRunner = ComponentMakerV0.RouteEmitter()
			auctioneerRunner = ComponentMakerV0.Auctioneer()
			repRunner = ComponentMakerV0.Rep()
			sshProxyRunner = ComponentMakerV0.SSHProxy()
		})

		JustBeforeEach(func() {
			ginkgomon.Invoke(ComponentMakerV1.Garden())
			bbs = ginkgomon.Invoke(bbsRunner)
			routeEmitter = ginkgomon.Invoke(routeEmitterRunner)
			auctioneer = ginkgomon.Invoke(auctioneerRunner)
			rep = ginkgomon.Invoke(repRunner)
			sshProxy = ginkgomon.Invoke(sshProxyRunner)
		})

		AfterEach(func() {
			destroyContainerErrors := helpers.CleanupGarden(ComponentMakerV1.GardenClient())

			helpers.StopProcesses(
				bbs,
				auctioneer,
				rep,
				routeEmitter,
				sshProxy,
				plumbing,
			)

			Expect(destroyContainerErrors).To(
				BeEmpty(),
				"%d containers failed to be destroyed!",
				len(destroyContainerErrors),
			)
		})

		Context("v0 configuration", func() {
			It("runs vizzini successfully", func() {
				runVizziniTests(bbsClientGoPathEnvVar, securityGroupV0Tests)
			})
		})

		Context("upgrading the BBS API", func() {
			BeforeEach(func() {
				skipLocket := func(cfg *bbsconfig.BBSConfig) {
					cfg.ClientLocketConfig.LocketAddress = ""
				}
				fallbackToHTTPAuctioneer := func(cfg *bbsconfig.BBSConfig) {
					cfg.AuctioneerRequireTLS = false
				}
				bbsRunner = ComponentMakerV1.BBS(skipLocket, fallbackToHTTPAuctioneer)
			})

			It("runs vizzini successfully", func() {
				runVizziniTests(bbsClientGoPathEnvVar, securityGroupV0Tests)
			})
		})

		Context("upgrading the BBS API and BBS client", func() {
			BeforeEach(func() {
				bbsClientGoPathEnvVar = "GOPATH"

				skipLocket := func(cfg *bbsconfig.BBSConfig) {
					cfg.ClientLocketConfig.LocketAddress = ""
				}
				fallbackToHTTPAuctioneer := func(cfg *bbsconfig.BBSConfig) {
					cfg.AuctioneerRequireTLS = false
				}
				bbsRunner = ComponentMakerV1.BBS(skipLocket, fallbackToHTTPAuctioneer)
			})

			It("runs vizzini successfully", func() {
				runVizziniTests(bbsClientGoPathEnvVar, repV0UnsupportedVizziniTests...)
			})
		})

		Context("upgrading the BBS API, BBS client, sshProxy, and Auctioneer", func() {
			BeforeEach(func() {
				bbsClientGoPathEnvVar = "GOPATH"
				skipLocket := func(cfg *bbsconfig.BBSConfig) {
					cfg.ClientLocketConfig.LocketAddress = ""
				}
				fallbackToHTTPAuctioneer := func(cfg *bbsconfig.BBSConfig) {
					cfg.AuctioneerRequireTLS = false
				}
				bbsRunner = ComponentMakerV1.BBS(skipLocket, fallbackToHTTPAuctioneer)
				auctioneerRunner = ComponentMakerV1.Auctioneer(func(cfg *auctioneerconfig.AuctioneerConfig) {
					cfg.ClientLocketConfig.LocketAddress = ""
				})
				sshProxyRunner = ComponentMakerV1.SSHProxy()
			})

			It("runs vizzini successfully", func() {
				runVizziniTests(bbsClientGoPathEnvVar, repV0UnsupportedVizziniTests...)
			})
		})

		Context("upgrading the BBS API, BBS client, sshProxy, Auctioneer, and Rep", func() {
			BeforeEach(func() {
				bbsClientGoPathEnvVar = "GOPATH"
				skipLocket := func(cfg *bbsconfig.BBSConfig) {
					cfg.ClientLocketConfig.LocketAddress = ""
				}
				fallbackToHTTPAuctioneer := func(cfg *bbsconfig.BBSConfig) {
					cfg.AuctioneerRequireTLS = false
				}
				bbsRunner = ComponentMakerV1.BBS(skipLocket, fallbackToHTTPAuctioneer)
				auctioneerRunner = ComponentMakerV1.Auctioneer(func(cfg *auctioneerconfig.AuctioneerConfig) {
					cfg.ClientLocketConfig.LocketAddress = ""
				})
				sshProxyRunner = ComponentMakerV1.SSHProxy()

				exportNetworkConfigs := func(cfg *repconfig.RepConfig) {
					cfg.ExportNetworkEnvVars = true
				}
				repRunner = ComponentMakerV1.Rep(exportNetworkConfigs)
			})

			It("runs vizzini successfully", func() {
				runVizziniTests(bbsClientGoPathEnvVar)
			})
		})

		Context("upgrading the BBS API, BBS client, sshProxy, Auctioneer, Rep, and Route Emitter", func() {
			BeforeEach(func() {
				bbsClientGoPathEnvVar = "GOPATH"
				skipLocket := func(cfg *bbsconfig.BBSConfig) {
					cfg.ClientLocketConfig.LocketAddress = ""
				}
				fallbackToHTTPAuctioneer := func(cfg *bbsconfig.BBSConfig) {
					cfg.AuctioneerRequireTLS = false
				}
				bbsRunner = ComponentMakerV1.BBS(skipLocket, fallbackToHTTPAuctioneer)
				auctioneerRunner = ComponentMakerV1.Auctioneer(func(cfg *auctioneerconfig.AuctioneerConfig) {
					cfg.ClientLocketConfig.LocketAddress = ""
				})
				sshProxyRunner = ComponentMakerV1.SSHProxy()

				exportNetworkConfigs := func(cfg *repconfig.RepConfig) {
					cfg.ExportNetworkEnvVars = true
				}
				repRunner = ComponentMakerV1.Rep(exportNetworkConfigs)
				routeEmitterRunner = ComponentMakerV1.RouteEmitter()
			})

			It("runs vizzini successfully", func() {
				runVizziniTests(bbsClientGoPathEnvVar)
			})
		})
	})

	Context("from v1.25.2", func() {
		BeforeEach(func() {
			logger = lager.NewLogger("test")
			logger.RegisterSink(lager.NewWriterSink(GinkgoWriter, lager.DEBUG))

			bbsClientGoPathEnvVar = "GOPATH_V0"

			ComponentMakerV0 = world.MakeComponentMaker("fixtures/certs/", oldArtifacts, addresses)

			fileServer, _ := ComponentMakerV1.FileServer()

			plumbing = ginkgomon.Invoke(grouper.NewParallel(os.Kill, grouper.Members{
				{Name: "nats", Runner: ComponentMakerV1.NATS()},
				{Name: "sql", Runner: ComponentMakerV1.SQL()},
				{Name: "consul", Runner: ComponentMakerV1.Consul()},
				{Name: "file-server", Runner: fileServer},
				{Name: "garden", Runner: ComponentMakerV1.Garden()},
				{Name: "router", Runner: ComponentMakerV1.Router()},
			}))
			helpers.ConsulWaitUntilReady(ComponentMakerV0.Addresses())

			locketRunner = ComponentMakerV0.Locket()
			bbsRunner = ComponentMakerV0.BBS()
			routeEmitterRunner = ComponentMakerV0.RouteEmitter()
			auctioneerRunner = ComponentMakerV0.Auctioneer()
			repRunner = ComponentMakerV0.Rep()
			sshProxyRunner = ComponentMakerV0.SSHProxy()
		})

		JustBeforeEach(func() {
			locket = ginkgomon.Invoke(locketRunner)
			bbs = ginkgomon.Invoke(bbsRunner)
			routeEmitter = ginkgomon.Invoke(routeEmitterRunner)
			auctioneer = ginkgomon.Invoke(auctioneerRunner)
			rep = ginkgomon.Invoke(repRunner)
			sshProxy = ginkgomon.Invoke(sshProxyRunner)
		})

		AfterEach(func() {
			destroyContainerErrors := helpers.CleanupGarden(ComponentMakerV1.GardenClient())

			helpers.StopProcesses(
				locket,
				bbs,
				auctioneer,
				rep,
				routeEmitter,
				sshProxy,
				plumbing,
			)

			Expect(destroyContainerErrors).To(
				BeEmpty(),
				"%d containers failed to be destroyed!",
				len(destroyContainerErrors),
			)
		})

		Context("v0 configuration", func() {
			FIt("runs vizzini successfully", func() {
				runVizziniTests(bbsClientGoPathEnvVar, securityGroupV0Tests)
			})
		})

		XContext("upgrading the BBS API", func() {
			BeforeEach(func() {
				fallbackToHTTPAuctioneer := func(cfg *bbsconfig.BBSConfig) {
					cfg.AuctioneerRequireTLS = false
				}
				bbsRunner = ComponentMakerV1.BBS(fallbackToHTTPAuctioneer)
			})

			It("runs vizzini successfully", func() {
				runVizziniTests(bbsClientGoPathEnvVar, securityGroupV0Tests)
			})
		})

		XContext("upgrading the BBS API and BBS client", func() {
			BeforeEach(func() {
				bbsClientGoPathEnvVar = "GOPATH"
				fallbackToHTTPAuctioneer := func(cfg *bbsconfig.BBSConfig) {
					cfg.AuctioneerRequireTLS = false
				}
				bbsRunner = ComponentMakerV1.BBS(fallbackToHTTPAuctioneer)
			})

			It("runs vizzini successfully", func() {
				runVizziniTests(bbsClientGoPathEnvVar, repV0UnsupportedVizziniTests...)
			})
		})

		XContext("upgrading the BBS API, BBS client, sshProxy, and Auctioneer", func() {
			BeforeEach(func() {
				bbsClientGoPathEnvVar = "GOPATH"
				fallbackToHTTPAuctioneer := func(cfg *bbsconfig.BBSConfig) {
					cfg.AuctioneerRequireTLS = false
				}
				bbsRunner = ComponentMakerV1.BBS(fallbackToHTTPAuctioneer)
				auctioneerRunner = ComponentMakerV1.Auctioneer(func(cfg *auctioneerconfig.AuctioneerConfig) {
					cfg.ClientLocketConfig.LocketAddress = ""
				})
				sshProxyRunner = ComponentMakerV1.SSHProxy()
			})

			It("runs vizzini successfully", func() {
				runVizziniTests(bbsClientGoPathEnvVar, repV0UnsupportedVizziniTests...)
			})
		})

		XContext("upgrading the BBS API, BBS client, sshProxy, Auctioneer, and Rep", func() {
			BeforeEach(func() {
				bbsClientGoPathEnvVar = "GOPATH"
				fallbackToHTTPAuctioneer := func(cfg *bbsconfig.BBSConfig) {
					cfg.AuctioneerRequireTLS = false
				}
				bbsRunner = ComponentMakerV1.BBS(fallbackToHTTPAuctioneer)
				auctioneerRunner = ComponentMakerV1.Auctioneer(func(cfg *auctioneerconfig.AuctioneerConfig) {
					cfg.ClientLocketConfig.LocketAddress = ""
				})
				sshProxyRunner = ComponentMakerV1.SSHProxy()

				exportNetworkConfigs := func(cfg *repconfig.RepConfig) {
					cfg.ExportNetworkEnvVars = true
				}
				repRunner = ComponentMakerV1.Rep(exportNetworkConfigs)
			})

			It("runs vizzini successfully", func() {
				runVizziniTests(bbsClientGoPathEnvVar)
			})
		})

		XContext("upgrading the BBS API, BBS client, sshProxy, Auctioneer, Rep, and Route Emitter", func() {
			BeforeEach(func() {
				bbsClientGoPathEnvVar = "GOPATH"
				fallbackToHTTPAuctioneer := func(cfg *bbsconfig.BBSConfig) {
					cfg.AuctioneerRequireTLS = false
				}
				bbsRunner = ComponentMakerV1.BBS(fallbackToHTTPAuctioneer)
				auctioneerRunner = ComponentMakerV1.Auctioneer(func(cfg *auctioneerconfig.AuctioneerConfig) {
					cfg.ClientLocketConfig.LocketAddress = ""
				})
				sshProxyRunner = ComponentMakerV1.SSHProxy()

				exportNetworkConfigs := func(cfg *repconfig.RepConfig) {
					cfg.ExportNetworkEnvVars = true
				}
				repRunner = ComponentMakerV1.Rep(exportNetworkConfigs)
				routeEmitterRunner = ComponentMakerV1.RouteEmitter()
			})

			It("runs vizzini successfully", func() {
				runVizziniTests(bbsClientGoPathEnvVar)
			})
		})
	})

})

func runVizziniTests(gopathEnvVar string, skips ...string) {
	ip, err := localip.LocalIP()
	Expect(err).NotTo(HaveOccurred())
	vizziniPath := filepath.Join(os.Getenv(gopathEnvVar), "src/code.cloudfoundry.org/vizzini")
	flags := []string{
		"-nodes", "4",
		"-randomizeAllSpecs",
		"-r",
		"-slowSpecThreshold", "60",
		// "-skip", strings.Join(skips, "|"),
		"--",
		"-bbs-address", "https://" + ComponentMakerV1.Addresses().BBS,
		"-bbs-client-cert", ComponentMakerV1.BBSSSLConfig().ClientCert,
		"-bbs-client-key", ComponentMakerV1.BBSSSLConfig().ClientKey,
		"-ssh-address", ComponentMakerV1.Addresses().SSHProxy,
		"-ssh-password", "",
		"-routable-domain-suffix", "test.internal", // Served by dnsmasq using setup_inigo script
		"-host-address", ip,
	}

	cmd := exec.Command("ginkgo", flags...)
	cmd.Dir = vizziniPath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	Expect(cmd.Run()).To(Succeed())
}
