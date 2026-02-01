// Package e2e contains end-to-end tests for cfgate.
package e2e_test

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	cloudflare "github.com/cloudflare/cloudflare-go/v6"
	"github.com/cloudflare/cloudflare-go/v6/dns"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gateway "sigs.k8s.io/gateway-api/apis/v1"

	cfgatev1alpha1 "cfgate.io/cfgate/api/v1alpha1"
)

var _ = Describe("CloudflareDNSSync E2E", func() {
	var (
		namespace  *corev1.Namespace
		tunnelName string
		cfClient   *cloudflare.Client
		zoneID     string

		// Track hostnames for cleanup.
		createdHostnames []string
	)

	BeforeEach(func() {
		skipIfNoZone() // Requires zone in addition to credentials.

		// Create unique namespace for this test.
		namespace = createTestNamespace("cfgate-dns-e2e")

		// Generate unique tunnel name.
		tunnelName = generateUniqueName("e2e-dns-tunnel")

		// Create Cloudflare credentials secret.
		createCloudflareCredentialsSecret(namespace.Name)

		// Create Cloudflare client for verification.
		cfClient = getCloudflareClient()

		// Get zone ID for DNS operations.
		var err error
		zoneID, err = getZoneIDByName(ctx, cfClient, testEnv.CloudflareZoneName)
		Expect(err).NotTo(HaveOccurred(), "Failed to get zone ID")
		Expect(zoneID).NotTo(BeEmpty())

		// Initialize hostname tracking.
		createdHostnames = []string{}
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

	Context("DNS record lifecycle", func() {
		It("should create CNAME record pointing to tunnel", func() {
			By("Creating CloudflareTunnel CR")
			tunnel := createCloudflareTunnel(ctx, k8sClient, "dns-tunnel", namespace.Name, tunnelName)
			tunnel = waitForTunnelReady(ctx, k8sClient, tunnel.Name, tunnel.Namespace, DefaultTimeout)
			tunnelDomain := tunnel.Status.TunnelDomain
			Expect(tunnelDomain).NotTo(BeEmpty())

			By("Creating CloudflareDNSSync CR")
			dnsSync := createCloudflareDNSSync(ctx, k8sClient, "dns-sync", namespace.Name, tunnel.Name)

			By("Creating GatewayClass")
			gcName := generateUniqueName("e2e-dns-gc")
			createGatewayClass(ctx, k8sClient, gcName)

			By("Creating Gateway referencing the tunnel")
			tunnelRef := fmt.Sprintf("%s/%s", namespace.Name, tunnel.Name)
			gw := createGateway(ctx, k8sClient, "dns-gateway", namespace.Name, gcName, tunnelRef)

			By("Creating a test Service")
			svc := createTestService(ctx, k8sClient, "dns-test-service", namespace.Name, 8080)

			By("Creating HTTPRoute with hostname")
			hostname := fmt.Sprintf("e2e-test-%s.%s", generateUniqueName("dns"), testEnv.CloudflareZoneName)
			createdHostnames = append(createdHostnames, hostname)
			createHTTPRoute(ctx, k8sClient, "dns-route", namespace.Name, gw.Name, []string{hostname}, svc.Name, 8080)

			By("Waiting for DNSSync to be ready")
			waitForDNSSyncReady(ctx, k8sClient, dnsSync.Name, dnsSync.Namespace, DefaultTimeout)

			By("Verifying DNS record exists in Cloudflare")
			Eventually(func() bool {
				record, err := getDNSRecordFromCloudflare(ctx, cfClient, zoneID, hostname, "CNAME")
				if err != nil {
					return false
				}
				return record != nil && record.Content == tunnelDomain
			}, DefaultTimeout, DefaultInterval).Should(BeTrue(), "DNS record should point to tunnel domain")

			By("Verifying record is proxied")
			record, err := getDNSRecordFromCloudflare(ctx, cfClient, zoneID, hostname, "CNAME")
			Expect(err).NotTo(HaveOccurred())
			Expect(record.Proxied).To(BeTrue(), "Record should be proxied")

			By("Verifying DNSSync status shows synced record")
			var updatedDNSSync cfgatev1alpha1.CloudflareDNSSync
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: dnsSync.Name, Namespace: dnsSync.Namespace}, &updatedDNSSync)).To(Succeed())
			Expect(updatedDNSSync.Status.SyncedRecords).To(BeNumerically(">=", 1))
		})

		It("should update DNS record when hostname changes", func() {
			By("Creating tunnel and DNSSync")
			tunnel := createCloudflareTunnel(ctx, k8sClient, "update-dns-tunnel", namespace.Name, tunnelName)
			tunnel = waitForTunnelReady(ctx, k8sClient, tunnel.Name, tunnel.Namespace, DefaultTimeout)

			dnsSync := createCloudflareDNSSync(ctx, k8sClient, "update-dns-sync", namespace.Name, tunnel.Name)

			By("Creating Gateway and HTTPRoute")
			gcName := generateUniqueName("e2e-update-gc")
			createGatewayClass(ctx, k8sClient, gcName)

			tunnelRef := fmt.Sprintf("%s/%s", namespace.Name, tunnel.Name)
			gw := createGateway(ctx, k8sClient, "update-dns-gateway", namespace.Name, gcName, tunnelRef)

			svc := createTestService(ctx, k8sClient, "update-dns-service", namespace.Name, 8080)

			hostname1 := fmt.Sprintf("e2e-update1-%s.%s", generateUniqueName("dns"), testEnv.CloudflareZoneName)
			createdHostnames = append(createdHostnames, hostname1)

			route := createHTTPRoute(ctx, k8sClient, "update-dns-route", namespace.Name, gw.Name, []string{hostname1}, svc.Name, 8080)

			By("Waiting for initial DNS record")
			waitForDNSSyncReady(ctx, k8sClient, dnsSync.Name, dnsSync.Namespace, DefaultTimeout)

			Eventually(func() bool {
				record, err := getDNSRecordFromCloudflare(ctx, cfClient, zoneID, hostname1, "CNAME")
				return err == nil && record != nil
			}, DefaultTimeout, DefaultInterval).Should(BeTrue())

			By("Updating HTTPRoute with new hostname")
			hostname2 := fmt.Sprintf("e2e-update2-%s.%s", generateUniqueName("dns"), testEnv.CloudflareZoneName)
			createdHostnames = append(createdHostnames, hostname2)

			// Fetch the HTTPRoute and update its hostname
			var updatedRoute gateway.HTTPRoute
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: route.Name, Namespace: route.Namespace}, &updatedRoute)).To(Succeed())
			updatedRoute.Spec.Hostnames = []gateway.Hostname{gateway.Hostname(hostname2)}
			Expect(k8sClient.Update(ctx, &updatedRoute)).To(Succeed())

			By("Waiting for new DNS record to be created")
			Eventually(func() bool {
				record, err := getDNSRecordFromCloudflare(ctx, cfClient, zoneID, hostname2, "CNAME")
				return err == nil && record != nil
			}, DefaultTimeout, DefaultInterval).Should(BeTrue(), "New DNS record should be created")
		})

		It("should delete DNS record when route is removed", func() {
			By("Creating tunnel and DNSSync")
			tunnel := createCloudflareTunnel(ctx, k8sClient, "delete-dns-tunnel", namespace.Name, tunnelName)
			tunnel = waitForTunnelReady(ctx, k8sClient, tunnel.Name, tunnel.Namespace, DefaultTimeout)

			dnsSync := createCloudflareDNSSync(ctx, k8sClient, "delete-dns-sync", namespace.Name, tunnel.Name)

			By("Creating Gateway and HTTPRoute")
			gcName := generateUniqueName("e2e-delete-gc")
			createGatewayClass(ctx, k8sClient, gcName)

			tunnelRef := fmt.Sprintf("%s/%s", namespace.Name, tunnel.Name)
			gw := createGateway(ctx, k8sClient, "delete-dns-gateway", namespace.Name, gcName, tunnelRef)

			svc := createTestService(ctx, k8sClient, "delete-dns-service", namespace.Name, 8080)

			hostname := fmt.Sprintf("e2e-delete-%s.%s", generateUniqueName("dns"), testEnv.CloudflareZoneName)
			createdHostnames = append(createdHostnames, hostname)

			route := createHTTPRoute(ctx, k8sClient, "delete-dns-route", namespace.Name, gw.Name, []string{hostname}, svc.Name, 8080)

			By("Waiting for DNS record to be created")
			waitForDNSSyncReady(ctx, k8sClient, dnsSync.Name, dnsSync.Namespace, DefaultTimeout)

			Eventually(func() bool {
				record, err := getDNSRecordFromCloudflare(ctx, cfClient, zoneID, hostname, "CNAME")
				return err == nil && record != nil
			}, DefaultTimeout, DefaultInterval).Should(BeTrue())

			By("Deleting the HTTPRoute")
			Expect(k8sClient.Delete(ctx, route)).To(Succeed())

			By("Waiting for DNS record to be deleted")
			Eventually(func() bool {
				record, err := getDNSRecordFromCloudflare(ctx, cfClient, zoneID, hostname, "CNAME")
				return err == nil && record == nil
			}, DefaultTimeout, DefaultInterval).Should(BeTrue(), "DNS record should be deleted when route is removed")
		})

		It("should only delete owned records (TXT ownership)", func() {
			By("Creating a pre-existing DNS record NOT owned by cfgate")
			preExistingHostname := fmt.Sprintf("e2e-preexisting-%s.%s", generateUniqueName("dns"), testEnv.CloudflareZoneName)
			createdHostnames = append(createdHostnames, preExistingHostname)

			// Create record directly via Cloudflare API (not managed by cfgate).
			_, err := cfClient.DNS.Records.New(ctx, dns.RecordNewParams{
				ZoneID: cloudflare.F(zoneID),
				Body: dns.CNAMERecordParam{
					Name:    cloudflare.F(preExistingHostname),
					Type:    cloudflare.F(dns.CNAMERecordTypeCNAME),
					Content: cloudflare.F("example.com"),
					TTL:     cloudflare.F(dns.TTL(1)),
					Proxied: cloudflare.F(true),
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Creating tunnel and DNSSync")
			tunnel := createCloudflareTunnel(ctx, k8sClient, "ownership-tunnel", namespace.Name, tunnelName)
			tunnel = waitForTunnelReady(ctx, k8sClient, tunnel.Name, tunnel.Namespace, DefaultTimeout)

			dnsSync := createCloudflareDNSSync(ctx, k8sClient, "ownership-dns-sync", namespace.Name, tunnel.Name)

			By("Creating Gateway and HTTPRoute with the same hostname")
			gcName := generateUniqueName("e2e-ownership-gc")
			createGatewayClass(ctx, k8sClient, gcName)

			tunnelRef := fmt.Sprintf("%s/%s", namespace.Name, tunnel.Name)
			gw := createGateway(ctx, k8sClient, "ownership-gateway", namespace.Name, gcName, tunnelRef)

			svc := createTestService(ctx, k8sClient, "ownership-service", namespace.Name, 8080)

			// Create route with the pre-existing hostname.
			route := createHTTPRoute(ctx, k8sClient, "ownership-route", namespace.Name, gw.Name, []string{preExistingHostname}, svc.Name, 8080)

			By("Waiting for DNSSync to process")
			waitForDNSSyncReady(ctx, k8sClient, dnsSync.Name, dnsSync.Namespace, DefaultTimeout)

			By("Deleting the HTTPRoute")
			Expect(k8sClient.Delete(ctx, route)).To(Succeed())

			By("Verifying pre-existing record is NOT deleted (not owned by cfgate)")
			// Wait a bit for any potential deletion to occur.
			Consistently(func() bool {
				record, err := getDNSRecordFromCloudflare(ctx, cfClient, zoneID, preExistingHostname, "CNAME")
				return err == nil && record != nil
			}, ShortTimeout, DefaultInterval).Should(BeTrue(), "Pre-existing record should NOT be deleted")

			By("Verifying pre-existing record still points to original target")
			record, err := getDNSRecordFromCloudflare(ctx, cfClient, zoneID, preExistingHostname, "CNAME")
			Expect(err).NotTo(HaveOccurred())
			Expect(record.Content).To(Equal("example.com"), "Pre-existing record content should be unchanged")
		})
	})

	Context("explicit hostnames", func() {
		It("should sync explicit hostnames from DNSSync spec", func() {
			By("Creating CloudflareTunnel CR")
			tunnel := createCloudflareTunnel(ctx, k8sClient, "explicit-tunnel", namespace.Name, tunnelName)
			tunnel = waitForTunnelReady(ctx, k8sClient, tunnel.Name, tunnel.Namespace, DefaultTimeout)
			tunnelDomain := tunnel.Status.TunnelDomain

			By("Creating CloudflareDNSSync with explicit hostname")
			explicitHostname := fmt.Sprintf("e2e-explicit-%s.%s", generateUniqueName("dns"), testEnv.CloudflareZoneName)
			createdHostnames = append(createdHostnames, explicitHostname)

			dnsSync := &cfgatev1alpha1.CloudflareDNSSync{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "explicit-dns-sync",
					Namespace: namespace.Name,
				},
				Spec: cfgatev1alpha1.CloudflareDNSSyncSpec{
					TunnelRef: cfgatev1alpha1.TunnelRef{
						Name: tunnel.Name,
					},
					Zones: []cfgatev1alpha1.ZoneConfig{
						{Name: testEnv.CloudflareZoneName},
					},
					Source: cfgatev1alpha1.HostnameSource{
						Explicit: []cfgatev1alpha1.ExplicitHostname{
							{
								Hostname: explicitHostname,
								Target:   "{{ .TunnelDomain }}",
								Proxied:  true,
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, dnsSync)).To(Succeed())

			By("Waiting for DNSSync to be ready")
			waitForDNSSyncReady(ctx, k8sClient, dnsSync.Name, dnsSync.Namespace, DefaultTimeout)

			By("Verifying explicit DNS record is created")
			Eventually(func() bool {
				record, err := getDNSRecordFromCloudflare(ctx, cfClient, zoneID, explicitHostname, "CNAME")
				if err != nil {
					return false
				}
				return record != nil && record.Content == tunnelDomain
			}, DefaultTimeout, DefaultInterval).Should(BeTrue(), "Explicit DNS record should be created")
		})
	})

	Context("DNSSync deletion", func() {
		It("should delete managed records when DNSSync is deleted", func() {
			By("Creating tunnel and DNSSync")
			tunnel := createCloudflareTunnel(ctx, k8sClient, "dnssync-delete-tunnel", namespace.Name, tunnelName)
			tunnel = waitForTunnelReady(ctx, k8sClient, tunnel.Name, tunnel.Namespace, DefaultTimeout)

			dnsSync := createCloudflareDNSSync(ctx, k8sClient, "dnssync-delete-sync", namespace.Name, tunnel.Name)

			By("Creating Gateway and HTTPRoute")
			gcName := generateUniqueName("e2e-dnssync-delete-gc")
			createGatewayClass(ctx, k8sClient, gcName)

			tunnelRef := fmt.Sprintf("%s/%s", namespace.Name, tunnel.Name)
			gw := createGateway(ctx, k8sClient, "dnssync-delete-gateway", namespace.Name, gcName, tunnelRef)

			svc := createTestService(ctx, k8sClient, "dnssync-delete-service", namespace.Name, 8080)

			hostname := fmt.Sprintf("e2e-dnssync-delete-%s.%s", generateUniqueName("dns"), testEnv.CloudflareZoneName)
			createdHostnames = append(createdHostnames, hostname)

			createHTTPRoute(ctx, k8sClient, "dnssync-delete-route", namespace.Name, gw.Name, []string{hostname}, svc.Name, 8080)

			By("Waiting for DNS record to be created")
			waitForDNSSyncReady(ctx, k8sClient, dnsSync.Name, dnsSync.Namespace, DefaultTimeout)

			Eventually(func() bool {
				record, err := getDNSRecordFromCloudflare(ctx, cfClient, zoneID, hostname, "CNAME")
				return err == nil && record != nil
			}, DefaultTimeout, DefaultInterval).Should(BeTrue())

			By("Deleting the CloudflareDNSSync")
			Expect(k8sClient.Delete(ctx, dnsSync)).To(Succeed())

			By("Waiting for DNS record to be deleted")
			Eventually(func() bool {
				record, err := getDNSRecordFromCloudflare(ctx, cfClient, zoneID, hostname, "CNAME")
				return err == nil && record == nil
			}, DefaultTimeout, DefaultInterval).Should(BeTrue(), "DNS record should be deleted when DNSSync is removed")
		})
	})

	Context("zone resolution", func() {
		It("should resolve zone by name", func() {
			By("Creating tunnel and DNSSync with zone by name")
			tunnel := createCloudflareTunnel(ctx, k8sClient, "zone-name-tunnel", namespace.Name, tunnelName)
			tunnel = waitForTunnelReady(ctx, k8sClient, tunnel.Name, tunnel.Namespace, DefaultTimeout)

			dnsSync := &cfgatev1alpha1.CloudflareDNSSync{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "zone-name-sync",
					Namespace: namespace.Name,
				},
				Spec: cfgatev1alpha1.CloudflareDNSSyncSpec{
					TunnelRef: cfgatev1alpha1.TunnelRef{
						Name: tunnel.Name,
					},
					Zones: []cfgatev1alpha1.ZoneConfig{
						{Name: testEnv.CloudflareZoneName}, // Zone by name, not ID.
					},
					Source: cfgatev1alpha1.HostnameSource{
						GatewayRoutes: cfgatev1alpha1.GatewayRoutesSource{
							Enabled: true,
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, dnsSync)).To(Succeed())

			By("Waiting for DNSSync to resolve zone and be ready")
			dnsSync = waitForDNSSyncReady(ctx, k8sClient, dnsSync.Name, dnsSync.Namespace, DefaultTimeout)

			By("Verifying ZonesResolved condition is True")
			var updatedSync cfgatev1alpha1.CloudflareDNSSync
			Expect(k8sClient.Get(ctx, client.ObjectKey{Name: dnsSync.Name, Namespace: dnsSync.Namespace}, &updatedSync)).To(Succeed())

			var zonesResolved bool
			for _, cond := range updatedSync.Status.Conditions {
				if cond.Type == "ZonesResolved" && cond.Status == "True" {
					zonesResolved = true
					break
				}
			}
			Expect(zonesResolved).To(BeTrue(), "ZonesResolved condition should be True")
		})
	})
})
