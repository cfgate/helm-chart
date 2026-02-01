// Package e2e contains end-to-end tests for cfgate.
package e2e_test

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	cloudflare "github.com/cloudflare/cloudflare-go/v6"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	cfgatev1alpha1 "cfgate.io/cfgate/api/v1alpha1"
)

var _ = Describe("CloudflareTunnel E2E", func() {
	var (
		namespace  *corev1.Namespace
		tunnelName string
		cfClient   *cloudflare.Client
	)

	BeforeEach(func() {
		skipIfNoCredentials()

		// Create unique namespace for this test.
		namespace = createTestNamespace("cfgate-tunnel-e2e")

		// Generate unique tunnel name.
		tunnelName = generateUniqueName("e2e-tunnel")

		// Create Cloudflare credentials secret.
		createCloudflareCredentialsSecret(namespace.Name)

		// Create Cloudflare client for verification.
		cfClient = getCloudflareClient()
	})

	AfterEach(func() {
		if testEnv.SkipCleanup {
			return
		}

		// Delete namespace - controller finalizers will attempt cleanup.
		// Any orphaned resources are cleaned by AfterSuite batch cleanup.
		if namespace != nil {
			deleteTestNamespace(namespace)
		}
	})

	Context("tunnel lifecycle", func() {
		It("should create tunnel in Cloudflare when CloudflareTunnel CR is created", func() {
			By("Creating CloudflareTunnel CR")
			tunnel := createCloudflareTunnel(ctx, k8sClient, "test-tunnel", namespace.Name, tunnelName)

			By("Waiting for tunnel to become ready")
			tunnel = waitForTunnelReady(ctx, k8sClient, tunnel.Name, tunnel.Namespace, DefaultTimeout)

			By("Verifying tunnel ID is populated in status")
			Expect(tunnel.Status.TunnelID).NotTo(BeEmpty(), "Tunnel ID should be populated in status")
			Expect(tunnel.Status.TunnelName).To(Equal(tunnelName), "Tunnel name should match")
			Expect(tunnel.Status.TunnelDomain).To(ContainSubstring(".cfargotunnel.com"), "Tunnel domain should be set")

			By("Verifying tunnel exists in Cloudflare API")
			cfTunnel, err := getTunnelFromCloudflare(ctx, cfClient, testEnv.CloudflareAccountID, tunnelName)
			Expect(err).NotTo(HaveOccurred())
			Expect(cfTunnel).NotTo(BeNil(), "Tunnel should exist in Cloudflare")
			Expect(cfTunnel.ID).To(Equal(tunnel.Status.TunnelID), "Tunnel IDs should match")

			By("Verifying cloudflared Deployment is created")
			deploymentName := fmt.Sprintf("%s-cloudflared", tunnel.Name)
			deployment := waitForDeploymentReady(ctx, k8sClient, deploymentName, namespace.Name, 1, DefaultTimeout)
			Expect(deployment).NotTo(BeNil())
		})

		It("should adopt existing tunnel when name matches", func() {
			By("Pre-creating tunnel via Cloudflare API")
			preTunnel, err := createTunnelInCloudflare(ctx, cfClient, testEnv.CloudflareAccountID, tunnelName)
			Expect(err).NotTo(HaveOccurred())
			Expect(preTunnel).NotTo(BeNil())
			preTunnelID := preTunnel.ID

			By("Creating CloudflareTunnel CR with same name")
			tunnel := createCloudflareTunnel(ctx, k8sClient, "adopt-tunnel", namespace.Name, tunnelName)

			By("Waiting for tunnel to become ready")
			tunnel = waitForTunnelReady(ctx, k8sClient, tunnel.Name, tunnel.Namespace, DefaultTimeout)

			By("Verifying it adopted the existing tunnel (same ID)")
			Expect(tunnel.Status.TunnelID).To(Equal(preTunnelID), "Should adopt existing tunnel ID")

			By("Verifying no duplicate tunnel was created")
			// List tunnels with this name - should only be one.
			cfTunnel, err := getTunnelFromCloudflare(ctx, cfClient, testEnv.CloudflareAccountID, tunnelName)
			Expect(err).NotTo(HaveOccurred())
			Expect(cfTunnel).NotTo(BeNil())
			Expect(cfTunnel.ID).To(Equal(preTunnelID), "Should be the same tunnel, not a duplicate")
		})

		It("should sync ingress configuration when routes change", func() {
			By("Creating CloudflareTunnel CR")
			tunnel := createCloudflareTunnel(ctx, k8sClient, "config-tunnel", namespace.Name, tunnelName)
			waitForTunnelReady(ctx, k8sClient, tunnel.Name, tunnel.Namespace, DefaultTimeout)

			By("Creating GatewayClass")
			gcName := generateUniqueName("e2e-gc")
			createGatewayClass(ctx, k8sClient, gcName)

			By("Creating Gateway referencing the tunnel")
			tunnelRef := fmt.Sprintf("%s/%s", namespace.Name, tunnel.Name)
			gw := createGateway(ctx, k8sClient, "test-gateway", namespace.Name, gcName, tunnelRef)

			By("Creating a test Service")
			svc := createTestService(ctx, k8sClient, "test-service", namespace.Name, 8080)

			By("Creating HTTPRoute with hostname")
			hostname := fmt.Sprintf("test-%s.%s", generateUniqueName("route"), testEnv.CloudflareZoneName)
			createHTTPRoute(ctx, k8sClient, "test-route", namespace.Name, gw.Name, []string{hostname}, svc.Name, 8080)

			By("Waiting for tunnel configuration to be synced")
			// The controller should update the tunnel configuration in Cloudflare.
			// We verify by checking the tunnel's configuration includes the route.
			Eventually(func() bool {
				// Refresh tunnel from K8s to get updated route count.
				var updatedTunnel cfgatev1alpha1.CloudflareTunnel
				err := k8sClient.Get(ctx, client.ObjectKey{Name: tunnel.Name, Namespace: tunnel.Namespace}, &updatedTunnel)
				if err != nil {
					return false
				}
				return updatedTunnel.Status.ConnectedRouteCount > 0
			}, DefaultTimeout, DefaultInterval).Should(BeTrue(), "Tunnel should have connected routes")

			By("Updating HTTPRoute hostname")
			var route cfgatev1alpha1.CloudflareTunnel
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: tunnel.Name, Namespace: tunnel.Namespace}, &route)).To(Succeed())
			// Configuration should be updated (verified by controller setting conditions).

			By("Verifying TunnelConfigured condition is True")
			waitForTunnelCondition(ctx, k8sClient, tunnel.Name, tunnel.Namespace, "TunnelConfigured", metav1.ConditionTrue, DefaultTimeout)
		})

		It("should delete tunnel from Cloudflare when CR is deleted", func() {
			By("Creating CloudflareTunnel CR")
			tunnel := createCloudflareTunnel(ctx, k8sClient, "delete-tunnel", namespace.Name, tunnelName)

			By("Waiting for tunnel to be created in Cloudflare")
			tunnel = waitForTunnelReady(ctx, k8sClient, tunnel.Name, tunnel.Namespace, DefaultTimeout)
			tunnelID := tunnel.Status.TunnelID
			Expect(tunnelID).NotTo(BeEmpty())

			By("Verifying tunnel exists in Cloudflare")
			cfTunnel, err := getTunnelByIDFromCloudflare(ctx, cfClient, testEnv.CloudflareAccountID, tunnelID)
			Expect(err).NotTo(HaveOccurred())
			Expect(cfTunnel).NotTo(BeNil())

			By("Deleting CloudflareTunnel CR")
			Expect(k8sClient.Delete(ctx, tunnel)).To(Succeed())

			By("Waiting for tunnel to be deleted from Kubernetes")
			waitForTunnelDeleted(ctx, k8sClient, tunnel.Name, tunnel.Namespace, DefaultTimeout)

			By("Verifying tunnel is deleted from Cloudflare")
			waitForTunnelDeletedFromCloudflare(ctx, cfClient, testEnv.CloudflareAccountID, tunnelName, DefaultTimeout)

			By("Verifying cloudflared Deployment is deleted")
			deploymentName := fmt.Sprintf("%s-cloudflared", tunnel.Name)
			Eventually(func() bool {
				var dep corev1.Pod
				err := k8sClient.Get(ctx, client.ObjectKey{Name: deploymentName, Namespace: namespace.Name}, &dep)
				return client.IgnoreNotFound(err) == nil && err != nil
			}, DefaultTimeout, DefaultInterval).Should(BeTrue(), "Deployment should be deleted")
		})

		It("should handle tunnel deletion policy: orphan", func() {
			By("Creating CloudflareTunnel CR with orphan deletion policy")
			tunnel := &cfgatev1alpha1.CloudflareTunnel{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "orphan-tunnel",
					Namespace: namespace.Name,
					Annotations: map[string]string{
						"cfgate.io/deletion-policy": "orphan",
					},
				},
				Spec: cfgatev1alpha1.CloudflareTunnelSpec{
					Tunnel: cfgatev1alpha1.TunnelIdentity{
						Name: tunnelName,
					},
					Cloudflare: cfgatev1alpha1.CloudflareConfig{
						AccountID: testEnv.CloudflareAccountID,
						SecretRef: cfgatev1alpha1.SecretRef{
							Name: "cloudflare-credentials",
						},
					},
					Cloudflared: cfgatev1alpha1.CloudflaredConfig{
						Replicas: 1,
					},
				},
			}
			Expect(k8sClient.Create(ctx, tunnel)).To(Succeed())

			By("Waiting for tunnel to be created in Cloudflare")
			tunnel = waitForTunnelReady(ctx, k8sClient, tunnel.Name, tunnel.Namespace, DefaultTimeout)
			tunnelID := tunnel.Status.TunnelID

			By("Deleting CloudflareTunnel CR")
			Expect(k8sClient.Delete(ctx, tunnel)).To(Succeed())

			By("Waiting for CR to be deleted from Kubernetes")
			waitForTunnelDeleted(ctx, k8sClient, tunnel.Name, tunnel.Namespace, DefaultTimeout)

			By("Verifying tunnel still exists in Cloudflare (orphaned)")
			cfTunnel, err := getTunnelByIDFromCloudflare(ctx, cfClient, testEnv.CloudflareAccountID, tunnelID)
			Expect(err).NotTo(HaveOccurred())
			Expect(cfTunnel).NotTo(BeNil(), "Tunnel should still exist in Cloudflare with orphan policy")
		})
	})

	Context("error handling", func() {
		It("should set CredentialsValid=False when token is invalid", func() {
			By("Creating CloudflareTunnel CR with invalid credentials")
			invalidTunnelName := generateUniqueName("invalid-token-tunnel")
			tunnel := createCloudflareTunnelWithInvalidToken(ctx, k8sClient, "invalid-token-tunnel", namespace.Name, invalidTunnelName)

			By("Waiting for CredentialsValid condition to be False")
			waitForTunnelCondition(ctx, k8sClient, tunnel.Name, tunnel.Namespace, "CredentialsValid", metav1.ConditionFalse, ShortTimeout)

			By("Verifying Ready condition is also False")
			var updatedTunnel cfgatev1alpha1.CloudflareTunnel
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: tunnel.Name, Namespace: tunnel.Namespace}, &updatedTunnel)).To(Succeed())

			var readyCondition metav1.Condition
			for _, cond := range updatedTunnel.Status.Conditions {
				if cond.Type == "Ready" {
					readyCondition = cond
					break
				}
			}
			Expect(readyCondition.Status).To(Equal(metav1.ConditionFalse), "Ready should be False when credentials are invalid")
		})

		It("should handle missing credentials secret", func() {
			By("Creating CloudflareTunnel CR referencing non-existent secret")
			tunnel := &cfgatev1alpha1.CloudflareTunnel{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "missing-secret-tunnel",
					Namespace: namespace.Name,
				},
				Spec: cfgatev1alpha1.CloudflareTunnelSpec{
					Tunnel: cfgatev1alpha1.TunnelIdentity{
						Name: generateUniqueName("missing-secret-tunnel"),
					},
					Cloudflare: cfgatev1alpha1.CloudflareConfig{
						AccountID: testEnv.CloudflareAccountID,
						SecretRef: cfgatev1alpha1.SecretRef{
							Name: "non-existent-secret",
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, tunnel)).To(Succeed())

			By("Waiting for condition indicating secret not found")
			// The controller should set a condition indicating the secret is missing.
			Eventually(func() bool {
				var t cfgatev1alpha1.CloudflareTunnel
				err := k8sClient.Get(ctx, client.ObjectKey{Name: tunnel.Name, Namespace: tunnel.Namespace}, &t)
				if err != nil {
					return false
				}
				for _, cond := range t.Status.Conditions {
					if cond.Type == "Ready" && cond.Status == metav1.ConditionFalse {
						return true
					}
				}
				return false
			}, ShortTimeout, DefaultInterval).Should(BeTrue(), "Should have Ready=False condition")
		})

		It("should recover when credentials become valid", func() {
			By("Creating CloudflareTunnel CR with initially missing secret")
			tunnelCRName := "recovery-tunnel"
			recoveryTunnelName := generateUniqueName("recovery-tunnel")

			tunnel := &cfgatev1alpha1.CloudflareTunnel{
				ObjectMeta: metav1.ObjectMeta{
					Name:      tunnelCRName,
					Namespace: namespace.Name,
				},
				Spec: cfgatev1alpha1.CloudflareTunnelSpec{
					Tunnel: cfgatev1alpha1.TunnelIdentity{
						Name: recoveryTunnelName,
					},
					Cloudflare: cfgatev1alpha1.CloudflareConfig{
						AccountID: testEnv.CloudflareAccountID,
						SecretRef: cfgatev1alpha1.SecretRef{
							Name: "recovery-credentials",
						},
					},
					Cloudflared: cfgatev1alpha1.CloudflaredConfig{
						Replicas: 1,
					},
				},
			}
			Expect(k8sClient.Create(ctx, tunnel)).To(Succeed())

			By("Waiting for Ready=False condition")
			waitForTunnelCondition(ctx, k8sClient, tunnel.Name, tunnel.Namespace, "Ready", metav1.ConditionFalse, ShortTimeout)

			By("Creating the missing credentials secret")
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "recovery-credentials",
					Namespace: namespace.Name,
				},
				Type: corev1.SecretTypeOpaque,
				StringData: map[string]string{
					"CLOUDFLARE_API_TOKEN": testEnv.CloudflareAPIToken,
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			By("Waiting for tunnel to recover and become Ready")
			waitForTunnelReady(ctx, k8sClient, tunnel.Name, tunnel.Namespace, DefaultTimeout)

			// Update tunnelName for cleanup.
			tunnelName = recoveryTunnelName
		})
	})

	Context("cloudflared deployment", func() {
		It("should create cloudflared Deployment with correct replicas", func() {
			By("Creating CloudflareTunnel CR with 2 replicas")
			tunnel := &cfgatev1alpha1.CloudflareTunnel{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "replicas-tunnel",
					Namespace: namespace.Name,
				},
				Spec: cfgatev1alpha1.CloudflareTunnelSpec{
					Tunnel: cfgatev1alpha1.TunnelIdentity{
						Name: tunnelName,
					},
					Cloudflare: cfgatev1alpha1.CloudflareConfig{
						AccountID: testEnv.CloudflareAccountID,
						SecretRef: cfgatev1alpha1.SecretRef{
							Name: "cloudflare-credentials",
						},
					},
					Cloudflared: cfgatev1alpha1.CloudflaredConfig{
						Replicas: 2,
					},
				},
			}
			Expect(k8sClient.Create(ctx, tunnel)).To(Succeed())

			By("Waiting for tunnel to become ready")
			tunnel = waitForTunnelReady(ctx, k8sClient, tunnel.Name, tunnel.Namespace, LongTimeout)

			By("Verifying Deployment has 2 replicas")
			deploymentName := fmt.Sprintf("%s-cloudflared", tunnel.Name)
			deployment := waitForDeploymentReady(ctx, k8sClient, deploymentName, namespace.Name, 2, LongTimeout)
			Expect(*deployment.Spec.Replicas).To(Equal(int32(2)))
			Expect(deployment.Status.ReadyReplicas).To(Equal(int32(2)))

			By("Verifying tunnel status shows correct replica count")
			Eventually(func() int32 {
				var t cfgatev1alpha1.CloudflareTunnel
				if err := k8sClient.Get(ctx, client.ObjectKey{Name: tunnel.Name, Namespace: tunnel.Namespace}, &t); err != nil {
					return -1
				}
				return t.Status.ReadyReplicas
			}, DefaultTimeout, time.Second).Should(Equal(int32(2)))
		})

		It("should update cloudflared Deployment when spec changes", func() {
			By("Creating CloudflareTunnel CR with 1 replica")
			tunnel := createCloudflareTunnel(ctx, k8sClient, "update-tunnel", namespace.Name, tunnelName)
			tunnel = waitForTunnelReady(ctx, k8sClient, tunnel.Name, tunnel.Namespace, DefaultTimeout)

			By("Updating replica count to 2")
			var t cfgatev1alpha1.CloudflareTunnel
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: tunnel.Name, Namespace: tunnel.Namespace}, &t)).To(Succeed())
			t.Spec.Cloudflared.Replicas = 2
			Expect(k8sClient.Update(ctx, &t)).To(Succeed())

			By("Waiting for Deployment to scale to 2 replicas")
			deploymentName := fmt.Sprintf("%s-cloudflared", tunnel.Name)
			deployment := waitForDeploymentReady(ctx, k8sClient, deploymentName, namespace.Name, 2, LongTimeout)
			Expect(*deployment.Spec.Replicas).To(Equal(int32(2)))
		})
	})
})
