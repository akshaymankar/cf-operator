package manifest_test

import (
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	"go.uber.org/zap"

	"code.cloudfoundry.org/cf-operator/pkg/bosh/manifest"
	. "code.cloudfoundry.org/cf-operator/pkg/bosh/manifest"
	helper "code.cloudfoundry.org/cf-operator/pkg/testhelper"
	"code.cloudfoundry.org/cf-operator/testing"
)

const assetPath = "../../../testing/assets"

var _ = Describe("DataGatherer", func() {

	var (
		m   *Manifest
		env testing.Catalog
		log *zap.SugaredLogger
		dg  *DataGatherer
	)

	Context("Job", func() {
		Describe("property helper to override job specs from manifest", func() {
			It("should find a property value in the manifest job properties section (constructed example)", func() {
				// health.disk.warning
				exampleJob := Job{
					Properties: JobProperties{
						Properties: map[string]interface{}{
							"health": map[interface{}]interface{}{
								"disk": map[interface{}]interface{}{
									"warning": 42,
								},
							},
						},
					},
				}

				value, ok := exampleJob.Property("health.disk.warning")
				Expect(ok).To(BeTrue())
				Expect(value).To(BeEquivalentTo(42))

				value, ok = exampleJob.Property("health.disk.nonexisting")
				Expect(ok).To(BeFalse())
				Expect(value).To(BeNil())
			})

			It("should find a property value in the manifest job properties section (proper manifest example)", func() {
				m = env.BOSHManifestWithProviderAndConsumer()
				job := m.InstanceGroups[0].Jobs[0]

				value, ok := job.Property("doppler.grpc_port")
				Expect(ok).To(BeTrue())
				Expect(value).To(BeEquivalentTo(7765))
			})
		})
	})

	Context("DataGatherer", func() {
		JustBeforeEach(func() {
			_, log = helper.NewTestLogger()
			dg = manifest.NewDataGatherer(log, "default", m)
		})

		Describe("GenerateManifest", func() {
			BeforeEach(func() {
				m = env.BOSHManifestWithProviderAndConsumer()
			})

			It("generates a manifest", func() {
				manifest, err := dg.GenerateManifest(assetPath, "log-api")
				Expect(err).ToNot(HaveOccurred())
				Expect(manifest).NotTo(BeEmpty())
				Expect(string(manifest)).To(ContainSubstring("- name: doppler"))
			})
		})

		Describe("CollectReleaseSpecsAndProviderLinks", func() {
			BeforeEach(func() {
				m = env.ElaboratedBOSHManifest()
			})

			It("should gather all data for each job spec file", func() {
				releaseSpecs, _, err := dg.CollectReleaseSpecsAndProviderLinks(assetPath)
				Expect(err).ToNot(HaveOccurred())
				Expect(len(releaseSpecs)).To(Equal(2))

				//Check releaseSpecs for the redis job.MF test file
				redisReleaseSpec := releaseSpecs["redis"]["redis-server"]
				Expect(len(redisReleaseSpec.Templates)).To(Equal(4))
				Expect(len(redisReleaseSpec.Properties)).To(Equal(12))
				Expect(redisReleaseSpec.Consumes[0]).To(MatchFields(IgnoreMissing, Fields{
					"Name":     Equal("redis"),
					"Type":     Equal("redis"),
					"Optional": Equal(true),
				}))
				Expect(redisReleaseSpec.Provides[0]).To(MatchFields(IgnoreExtras, Fields{
					"Name":       Equal("redis"),
					"Type":       Equal("redis"),
					"Properties": ConsistOf("port", "password", "base_dir"),
				}))

				//Check releaseSpecs for the cflinuxfs3 job.MF test file
				cfLinuxReleaseSpec := releaseSpecs["cflinuxfs3"]["cflinuxfs3-rootfs-setup"]
				Expect(len(cfLinuxReleaseSpec.Templates)).To(Equal(2))
				Expect(len(cfLinuxReleaseSpec.Properties)).To(Equal(1))
				Expect(len(cfLinuxReleaseSpec.Consumes)).To(Equal(0))
				Expect(len(cfLinuxReleaseSpec.Provides)).To(Equal(0))
			})

			It("should have properties/bosh_containerization/instances populated for each job", func() {
				_, _, err := dg.CollectReleaseSpecsAndProviderLinks(assetPath)
				Expect(err).ToNot(HaveOccurred())

				//Check JobInstance for the redis-server job
				jobInstancesRedis := m.InstanceGroups[0].Jobs[0].Properties.BOSHContainerization.Instances

				compareToFakeRedis := []JobInstance{
					{Address: "foo-deployment-redis-slave-0.default.svc.cluster.local", AZ: "z1", ID: "redis-slave-0-redis-server", Index: 0, Instance: 0, Name: "redis-slave-redis-server"},
					{Address: "foo-deployment-redis-slave-1.default.svc.cluster.local", AZ: "z2", ID: "redis-slave-1-redis-server", Index: 1, Instance: 0, Name: "redis-slave-redis-server"},
					{Address: "foo-deployment-redis-slave-2.default.svc.cluster.local", AZ: "z1", ID: "redis-slave-2-redis-server", Index: 2, Instance: 1, Name: "redis-slave-redis-server"},
					{Address: "foo-deployment-redis-slave-3.default.svc.cluster.local", AZ: "z2", ID: "redis-slave-3-redis-server", Index: 3, Instance: 1, Name: "redis-slave-redis-server"},
				}
				Expect(jobInstancesRedis).To(BeEquivalentTo(compareToFakeRedis))

				_, _, err = dg.CollectReleaseSpecsAndProviderLinks(assetPath)
				Expect(err).ToNot(HaveOccurred())
				//Check JobInstance for the cflinuxfs3-rootfs-setup job
				jobInstancesCell := m.InstanceGroups[1].Jobs[0].Properties.BOSHContainerization.Instances

				compareToFakeCell := []JobInstance{
					{Address: "foo-deployment-diego-cell-0.default.svc.cluster.local", AZ: "z1", ID: "diego-cell-0-cflinuxfs3-rootfs-setup", Index: 0, Instance: 0, Name: "diego-cell-cflinuxfs3-rootfs-setup"},
					{Address: "foo-deployment-diego-cell-1.default.svc.cluster.local", AZ: "z2", ID: "diego-cell-1-cflinuxfs3-rootfs-setup", Index: 1, Instance: 0, Name: "diego-cell-cflinuxfs3-rootfs-setup"},
					{Address: "foo-deployment-diego-cell-2.default.svc.cluster.local", AZ: "z1", ID: "diego-cell-2-cflinuxfs3-rootfs-setup", Index: 2, Instance: 1, Name: "diego-cell-cflinuxfs3-rootfs-setup"},
					{Address: "foo-deployment-diego-cell-3.default.svc.cluster.local", AZ: "z2", ID: "diego-cell-3-cflinuxfs3-rootfs-setup", Index: 3, Instance: 1, Name: "diego-cell-cflinuxfs3-rootfs-setup"},
				}
				Expect(jobInstancesCell).To(BeEquivalentTo(compareToFakeCell))
			})

			It("should get all links from providers", func() {
				_, providerLinks, err := dg.CollectReleaseSpecsAndProviderLinks(assetPath)
				Expect(err).ToNot(HaveOccurred())
				Expect(len(providerLinks)).To(BeEquivalentTo(1))
				expectedInstances := []JobInstance{
					{Address: "foo-deployment-redis-slave-0.default.svc.cluster.local", AZ: "z1", ID: "redis-slave-0-redis-server", Index: 0, Instance: 0, Name: "redis-slave-redis-server"},
					{Address: "foo-deployment-redis-slave-1.default.svc.cluster.local", AZ: "z2", ID: "redis-slave-1-redis-server", Index: 1, Instance: 0, Name: "redis-slave-redis-server"},
					{Address: "foo-deployment-redis-slave-2.default.svc.cluster.local", AZ: "z1", ID: "redis-slave-2-redis-server", Index: 2, Instance: 1, Name: "redis-slave-redis-server"},
					{Address: "foo-deployment-redis-slave-3.default.svc.cluster.local", AZ: "z2", ID: "redis-slave-3-redis-server", Index: 3, Instance: 1, Name: "redis-slave-redis-server"},
				}
				expectedProperties := map[string]interface{}{
					"port":     6379,
					"password": "foobar",
					"base_dir": "/var/vcap/store/redis",
				}
				//Check that Instances in the link are correct
				Expect(providerLinks["redis"]["redis-server"].Instances).To(BeEquivalentTo(expectedInstances))
				Expect(providerLinks["redis"]["redis-server"].Properties).To(BeEquivalentTo(expectedProperties))
			})
		})

		Describe("ProcessConsumersAndRenderBPM", func() {
			Context("when resolving links between providers and consumers", func() {
				BeforeEach(func() {
					m = env.BOSHManifestWithProviderAndConsumer()
				})

				It("should get all required data if the job consumes a link", func() {
					releaseSpecs, links, _ := dg.CollectReleaseSpecsAndProviderLinks(assetPath)
					_, err := dg.ProcessConsumersAndRenderBPM(assetPath, releaseSpecs, links, "log-api")
					Expect(err).ToNot(HaveOccurred())

					// log-api instance_group, with loggregator_trafficcontroller job, consumes a link from
					// doppler job
					jobBoshContainerizationConsumes := m.InstanceGroups[1].Jobs[0].Properties.BOSHContainerization.Consumes

					Expect(len(releaseSpecs)).To(Equal(1)) // only one release in the manifest.yml sample

					jobConsumesFromDoppler, consumeFromDopplerExists := jobBoshContainerizationConsumes["doppler"]
					Expect(consumeFromDopplerExists).To(BeTrue())

					expectedProperties := map[string]interface{}{
						"doppler": map[interface{}]interface{}{
							"grpc_port": 7765,
						},
						"fooprop": 10001,
					}

					for i, instance := range jobConsumesFromDoppler.Instances {
						Expect(instance.Index).To(Equal(i))
						Expect(instance.Address).To(Equal(fmt.Sprintf("cf-doppler-%v.default.svc.cluster.local", i)))
						Expect(instance.ID).To(Equal(fmt.Sprintf("doppler-%v-doppler", i)))
					}
					Expect(jobConsumesFromDoppler.Properties).To(BeEquivalentTo(expectedProperties))
				})

				It("should get nothing if the job does not consumes a link", func() {
					releaseSpecs, links, _ := dg.CollectReleaseSpecsAndProviderLinks(assetPath)
					_, err := dg.ProcessConsumersAndRenderBPM(assetPath, releaseSpecs, links, "log-api")

					// doppler instance_group, with doppler job, only provides doppler link
					jobBoshContainerizationConsumes := m.InstanceGroups[0].Jobs[0].Properties.BOSHContainerization.Consumes
					var emptyJobBoshContainerizationConsumes map[string]JobLink

					Expect(err).ToNot(HaveOccurred())
					Expect(jobBoshContainerizationConsumes).To(BeEquivalentTo(emptyJobBoshContainerizationConsumes))
				})
			})
		})

		Context("when rendering ERB files", func() {
			BeforeEach(func() {
				m = env.BOSHManifestWithProviderAndConsumer()
			})

			It("should render complex ERB files", func() {
				releaseSpecs, links, err := dg.CollectReleaseSpecsAndProviderLinks(assetPath)
				Expect(err).ToNot(HaveOccurred())
				_, err = dg.ProcessConsumersAndRenderBPM(assetPath, releaseSpecs, links, "log-api")
				Expect(err).ToNot(HaveOccurred())

				jobBoshContainerizationPropertiesInstances := m.InstanceGroups[1].Jobs[0].Properties.BOSHContainerization.Instances
				Expect(len(jobBoshContainerizationPropertiesInstances)).To(Equal(4))

				// in ERB files, there are test environment variables like these:
				//   FOOBARWITHLINKVALUES: <%= link('doppler').p("fooprop") %>
				//   FOOBARWITHLINKNESTEDVALUES: <%= link('doppler').p("doppler.grpc_port") %>
				//   FOOBARWITHLINKINSTANCESINDEX: <%= link('doppler').instances[0].index %>
				//   FOOBARWITHLINKINSTANCESAZ: <%= link('doppler').instances[0].az %>
				//   FOOBARWITHLINKINSTANCESADDRESS: <%= link('doppler').instances[0].address %>
				//   ...

				// For the first instance
				bpmProcesses := m.InstanceGroups[1].Jobs[0].Properties.BOSHContainerization.BPM.Processes[0]

				Expect(bpmProcesses.Env["FOOBARWITHLINKVALUES"]).To(Equal("10001"))
				Expect(bpmProcesses.Env["FOOBARWITHLINKNESTEDVALUES"]).To(Equal("7765"))
				Expect(bpmProcesses.Env["FOOBARWITHLINKINSTANCESAZ"]).To(Equal("z1"))
				Expect(bpmProcesses.Env["FOOBARWITHLINKINSTANCESADDRESS"]).To(Equal("cf-doppler-0.default.svc.cluster.local"))
				Expect(bpmProcesses.Env["FOOBARWITHSPECADDRESS"]).To(Equal("cf-log-api-0.default.svc.cluster.local"))
				Expect(bpmProcesses.Env["FOOBARWITHSPECDEPLOYMENT"]).To(Equal("cf"))

				// For the second instance
				bpmProcesses = m.InstanceGroups[1].Jobs[0].Properties.BOSHContainerization.BPM.Processes[0]
				Expect(bpmProcesses.Env["FOOBARWITHSPECADDRESS"]).To(Equal("cf-log-api-0.default.svc.cluster.local"))

				// For the third instance
				bpmProcesses = m.InstanceGroups[1].Jobs[0].Properties.BOSHContainerization.BPM.Processes[0]
				Expect(bpmProcesses.Env["FOOBARWITHSPECADDRESS"]).To(Equal("cf-log-api-0.default.svc.cluster.local"))

				// For the fourth instance
				bpmProcesses = m.InstanceGroups[1].Jobs[0].Properties.BOSHContainerization.BPM.Processes[0]
				Expect(bpmProcesses.Env["FOOBARWITHSPECADDRESS"]).To(Equal("cf-log-api-0.default.svc.cluster.local"))

			})
		})
	})
})
