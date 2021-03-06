package kube_test

import (
	b64 "encoding/base64"
	"os"
	"path/filepath"
	"strings"

	"code.cloudfoundry.org/cf-operator/testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Examples", func() {

	Describe("when examples are specified in the docs", func() {

		var (
			kubectlHelper *testing.Kubectl
			namespace     string
		)
		kubectlHelper = testing.NewKubectl()

		BeforeEach(func() {
			namespace = env.Namespace
		})

		const examplesDir = "../../docs/examples/"

		Context("all examples must be working", func() {

			It("extended-job ready example must work", func() {

				yamlFilePath := examplesDir + "extended-job/exjob_trigger_ready.yaml"

				By("Creating exjob_trigger")
				err := kubectlHelper.Create(namespace, yamlFilePath)
				Expect(err).ToNot(HaveOccurred())

				yamlPodFilePath := examplesDir + "extended-job/pod.yaml"

				By("Creating pod")
				kubectlHelper = testing.NewKubectl()
				err = kubectlHelper.Create(namespace, yamlPodFilePath)
				Expect(err).ToNot(HaveOccurred())

				By("Waiting for the pods to run")
				err = kubectlHelper.Wait(namespace, "ready", "pod/foo-pod-1")
				Expect(err).ToNot(HaveOccurred())

				err = kubectlHelper.WaitLabelFilter(namespace, "ready", "pod", "ejob-name=ready-triggered-sleep")
				Expect(err).ToNot(HaveOccurred())

				err = kubectlHelper.WaitLabelFilter(namespace, "complete", "pod", "ejob-name=ready-triggered-sleep")
				Expect(err).ToNot(HaveOccurred())

				By("Clean up resources")
				err = kubectlHelper.DeleteResource(namespace, "pod", "foo-pod-1")
				Expect(err).ToNot(HaveOccurred())

				err = kubectlHelper.Delete(namespace, yamlFilePath)
				Expect(err).ToNot(HaveOccurred())

				err = kubectlHelper.DeleteLabelFilter(namespace, "pod", "ejob-name=ready-triggered-sleep")
				Expect(err).ToNot(HaveOccurred())
			})

			It("extended-statefulset configs example must work", func() {

				yamlFilePath := examplesDir + "extended-statefulset/exstatefulset_configs.yaml"

				By("Creating exstatefulset configs")
				kubectlHelper := testing.NewKubectl()
				err := kubectlHelper.Create(namespace, yamlFilePath)
				Expect(err).ToNot(HaveOccurred())

				By("Checking for pods")
				err = kubectlHelper.Wait(namespace, "ready", "pod/example-extendedstatefulset-v1-0")
				Expect(err).ToNot(HaveOccurred())

				err = kubectlHelper.Wait(namespace, "ready", "pod/example-extendedstatefulset-v1-1")
				Expect(err).ToNot(HaveOccurred())

				yamlUpdatedFilePath := examplesDir + "extended-statefulset/exstatefulset_configs_updated.yaml"

				By("Updating the config value used by pods")
				err = kubectlHelper.Apply(namespace, yamlUpdatedFilePath)
				Expect(err).ToNot(HaveOccurred())

				By("Checking for pods")
				err = kubectlHelper.Wait(namespace, "ready", "pod/example-extendedstatefulset-v1-1")
				Expect(err).ToNot(HaveOccurred())

				err = kubectlHelper.Wait(namespace, "ready", "pod/example-extendedstatefulset-v1-0")
				Expect(err).ToNot(HaveOccurred())

				By("Checking the updated value in the env")
				err = kubectlHelper.RunCommandWithCheckString(namespace, "example-extendedstatefulset-v1-0", "env", "SPECIAL_KEY=value1Updated")
				Expect(err).ToNot(HaveOccurred())

				err = kubectlHelper.RunCommandWithCheckString(namespace, "example-extendedstatefulset-v1-1", "env", "SPECIAL_KEY=value1Updated")
				Expect(err).ToNot(HaveOccurred())

				By("Clean up resources")
				err = kubectlHelper.Delete(namespace, yamlFilePath)
				Expect(err).ToNot(HaveOccurred())

			})

			It("bosh-deployment example must work", func() {

				yamlFilePath := examplesDir + "bosh-deployment/boshdeployment.yaml"

				By("Creating bosh deployment")
				kubectlHelper := testing.NewKubectl()
				err := kubectlHelper.Create(namespace, yamlFilePath)
				Expect(err).ToNot(HaveOccurred())

				By("Checking for pods")
				err = kubectlHelper.Wait(namespace, "ready", "pod/nats-deployment-nats-v1-0")
				Expect(err).ToNot(HaveOccurred())

				err = kubectlHelper.Wait(namespace, "ready", "pod/nats-deployment-nats-v1-1")
				Expect(err).ToNot(HaveOccurred())

				By("Clean up resources")

				err = kubectlHelper.Delete(namespace, yamlFilePath)
				Expect(err).ToNot(HaveOccurred())

				err = kubectlHelper.DeleteResource(namespace, "secret", "nats-deployment.ig-resolved.nats-v1")
				Expect(err).ToNot(HaveOccurred())

				err = kubectlHelper.DeleteResource(namespace, "secret", "nats-deployment.with-ops")
				Expect(err).ToNot(HaveOccurred())

				err = kubectlHelper.DeleteResource(namespace, "secret", "nats-deployment.with-vars.interpolation-v1")
				Expect(err).ToNot(HaveOccurred())
			})

			It("bosh-deployment with customed variale example must work", func() {

				yamlFilePath := examplesDir + "bosh-deployment/boshdeployment-with-custom-variable.yaml"

				By("Creating bosh deployment")
				kubectlHelper := testing.NewKubectl()
				err := kubectlHelper.Create(namespace, yamlFilePath)
				Expect(err).ToNot(HaveOccurred())

				By("Checking for pods")
				err = kubectlHelper.Wait(namespace, "ready", "pod/nats-deployment-nats-v1-0")
				Expect(err).ToNot(HaveOccurred())

				err = kubectlHelper.Wait(namespace, "ready", "pod/nats-deployment-nats-v1-1")
				Expect(err).ToNot(HaveOccurred())

				By("Checking the value in the config file")
				outFile, err := kubectlHelper.RunCommandWithOutput(namespace, "nats-deployment-nats-v1-1", "awk 'NR == 18 {print substr($2,2,64)}' /var/vcap/jobs/nats/config/nats.conf")
				Expect(err).ToNot(HaveOccurred())

				outSecret, err := kubectlHelper.GetSecretData(namespace, "nats-deployment.var-customed-password", "go-template={{.data.password}}")
				Expect(err).ToNot(HaveOccurred())
				outSecretDecoded, _ := b64.StdEncoding.DecodeString(string(outSecret))
				Expect(string(outSecretDecoded)).To(Equal(strings.TrimSuffix(outFile, "\n")))

				By("Clean up resources")

				err = kubectlHelper.Delete(namespace, yamlFilePath)
				Expect(err).ToNot(HaveOccurred())

				err = kubectlHelper.DeleteResource(namespace, "secret", "nats-deployment.with-ops")
				Expect(err).ToNot(HaveOccurred())

				err = kubectlHelper.DeleteResource(namespace, "secret", "nats-deployment.ig-resolved.nats-v1")
				Expect(err).ToNot(HaveOccurred())

				err = kubectlHelper.DeleteResource(namespace, "secret", "nats-deployment.with-vars.interpolation-v1")
				Expect(err).ToNot(HaveOccurred())
			})

			It("extended-job auto errand delete example must work", func() {

				yamlFilePath := examplesDir + "extended-job/exjob_auto-errand-deletes-pod.yaml"

				By("Creating exjob")
				kubectlHelper := testing.NewKubectl()
				err := kubectlHelper.Create(namespace, yamlFilePath)
				Expect(err).ToNot(HaveOccurred())

				By("Checking for pods")
				err = kubectlHelper.WaitLabelFilter(namespace, "ready", "pod", "ejob-name=deletes-pod-1")
				Expect(err).ToNot(HaveOccurred())

				err = kubectlHelper.WaitLabelFilter(namespace, "terminate", "pod", "ejob-name=deletes-pod-1")
				Expect(err).ToNot(HaveOccurred())

				By("Clean up resources")
				err = kubectlHelper.Delete(namespace, yamlFilePath)
				Expect(err).ToNot(HaveOccurred())
			})

			It("extended-job auto errand example must work", func() {

				yamlFilePath := examplesDir + "extended-job/exjob_auto-errand.yaml"

				By("Creating exjob")
				kubectlHelper := testing.NewKubectl()
				err := kubectlHelper.Create(namespace, yamlFilePath)
				Expect(err).ToNot(HaveOccurred())

				By("Checking for pods")
				err = kubectlHelper.WaitLabelFilter(namespace, "ready", "pod", "ejob-name=one-time-sleep")
				Expect(err).ToNot(HaveOccurred())

				err = kubectlHelper.WaitLabelFilter(namespace, "complete", "pod", "ejob-name=one-time-sleep")
				Expect(err).ToNot(HaveOccurred())

				By("Clean up resources")
				err = kubectlHelper.Delete(namespace, yamlFilePath)
				Expect(err).ToNot(HaveOccurred())

				err = kubectlHelper.DeleteLabelFilter(namespace, "pod", "ejob-name=one-time-sleep")
				Expect(err).ToNot(HaveOccurred())
			})

			It("extended-job auto errand update example must work", func() {

				yamlFilePath := examplesDir + "extended-job/exjob_auto-errand-updating.yaml"

				By("Creating exjob")
				kubectlHelper := testing.NewKubectl()
				err := kubectlHelper.Create(namespace, yamlFilePath)
				Expect(err).ToNot(HaveOccurred())

				By("Checking for pods")
				err = kubectlHelper.WaitLabelFilter(namespace, "ready", "pod", "ejob-name=auto-errand-sleep-again")
				Expect(err).ToNot(HaveOccurred())

				err = kubectlHelper.WaitLabelFilter(namespace, "complete", "pod", "ejob-name=auto-errand-sleep-again")
				Expect(err).ToNot(HaveOccurred())

				By("Delete the pod")
				err = kubectlHelper.DeleteLabelFilter(namespace, "pod", "ejob-name=auto-errand-sleep-again")
				Expect(err).ToNot(HaveOccurred())

				By("Update the config change")
				yamlFilePath = examplesDir + "extended-job/exjob_auto-errand-updating_updated.yaml"

				err = kubectlHelper.Apply(namespace, yamlFilePath)
				Expect(err).ToNot(HaveOccurred())

				err = kubectlHelper.WaitLabelFilter(namespace, "ready", "pod", "ejob-name=auto-errand-sleep-again")
				Expect(err).ToNot(HaveOccurred())

				err = kubectlHelper.WaitLabelFilter(namespace, "complete", "pod", "ejob-name=auto-errand-sleep-again")
				Expect(err).ToNot(HaveOccurred())

				By("Clean up resources")
				err = kubectlHelper.Delete(namespace, yamlFilePath)
				Expect(err).ToNot(HaveOccurred())

				By("Delete the pod")
				err = kubectlHelper.DeleteLabelFilter(namespace, "pod", "ejob-name=auto-errand-sleep-again")
				Expect(err).ToNot(HaveOccurred())
			})

			It("extended-job errand example must work", func() {

				yamlFilePath := examplesDir + "extended-job/exjob_errand.yaml"

				By("Creating exjob")
				kubectlHelper := testing.NewKubectl()
				err := kubectlHelper.Create(namespace, yamlFilePath)
				Expect(err).ToNot(HaveOccurred())

				By("Updating exjob to trigger now")

				yamlFilePath = examplesDir + "extended-job/exjob_errand_updated.yaml"

				err = kubectlHelper.Apply(namespace, yamlFilePath)
				Expect(err).ToNot(HaveOccurred())

				By("Checking for pods")
				err = kubectlHelper.WaitLabelFilter(namespace, "ready", "pod", "ejob-name=manual-sleep")
				Expect(err).ToNot(HaveOccurred())

				err = kubectlHelper.WaitLabelFilter(namespace, "complete", "pod", "ejob-name=manual-sleep")
				Expect(err).ToNot(HaveOccurred())

				By("Clean up resources")
				err = kubectlHelper.DeleteLabelFilter(namespace, "pod", "ejob-name=manual-sleep")
				Expect(err).ToNot(HaveOccurred())

				err = kubectlHelper.Delete(namespace, yamlFilePath)
				Expect(err).ToNot(HaveOccurred())
			})

			It("extended-job output example must work", func() {

				yamlFilePath := examplesDir + "extended-job/exjob_output.yaml"

				By("Creating exjob")
				kubectlHelper := testing.NewKubectl()
				err := kubectlHelper.Create(namespace, yamlFilePath)
				Expect(err).ToNot(HaveOccurred())

				By("Checking for pods")
				err = kubectlHelper.WaitLabelFilter(namespace, "complete", "pod", "ejob-name=myfoo")
				Expect(err).ToNot(HaveOccurred())

				By("Checking the secret data created")
				outSecret, err := kubectlHelper.GetSecretData(namespace, "foo-json", "go-template={{.data.foo}}")
				Expect(err).ToNot(HaveOccurred())
				outSecretDecoded, _ := b64.StdEncoding.DecodeString(string(outSecret))
				Expect(string(outSecretDecoded)).To(Equal("1"))

				By("Clean up resources")
				err = kubectlHelper.DeleteLabelFilter(namespace, "pod", "ejob-name=myfoo")
				Expect(err).ToNot(HaveOccurred())

				err = kubectlHelper.Delete(namespace, yamlFilePath)
				Expect(err).ToNot(HaveOccurred())
			})

			It("extended-secret example must work", func() {

				yamlFilePath := examplesDir + "extended-secret/password.yaml"

				By("Creating an ExtendedSecret")
				kubectlHelper := testing.NewKubectl()
				err := kubectlHelper.Create(namespace, yamlFilePath)
				Expect(err).ToNot(HaveOccurred())

				By("Checking the generated password")
				err = kubectlHelper.SecretCheckData(namespace, "gen-secret1", ".data.password")
				Expect(err).ToNot(HaveOccurred())

				By("Clean up resources")
				err = kubectlHelper.Delete(namespace, yamlFilePath)
				Expect(err).ToNot(HaveOccurred())
			})

			It("Test cases must be written for all example use cases in docs", func() {

				countFile := 0
				err := filepath.Walk(examplesDir, func(path string, info os.FileInfo, err error) error {
					if !info.IsDir() {
						countFile = countFile + 1
					}
					return nil
				})
				Expect(err).NotTo(HaveOccurred())
				// If this testcase fails that means a test case is missing for an example in the docs folder
				Expect(countFile).To(Equal(22))
			})
		})
	})
})
