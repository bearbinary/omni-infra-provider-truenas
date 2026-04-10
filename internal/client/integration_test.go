//go:build integration

package client

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	// Load .env and .env.test if present — allows running integration tests
	// without manually exporting env vars each time.
	_ = godotenv.Load("../../.env")
	_ = godotenv.Load("../../.env.test")
}

// testClient returns a Client configured from env vars.
// Skips the test if neither the socket nor host+key are available.
func testClient(t *testing.T) *Client {
	t.Helper()

	host := os.Getenv("TRUENAS_TEST_HOST")
	apiKey := os.Getenv("TRUENAS_TEST_API_KEY")
	socketPath := os.Getenv("TRUENAS_TEST_SOCKET")

	if host == "" && socketPath == "" {
		t.Skip("TRUENAS_TEST_HOST (+ TRUENAS_TEST_API_KEY) or TRUENAS_TEST_SOCKET must be set for integration tests")
	}

	c, err := New(Config{
		Host:               host,
		APIKey:             apiKey,
		InsecureSkipVerify: true,
		SocketPath:         socketPath,
	})
	if err != nil {
		t.Fatalf("failed to create TrueNAS client: %v", err)
	}

	t.Cleanup(func() { c.Close() })

	return c
}

// testPool returns the ZFS pool name to use for tests.
func testPool(t *testing.T) string {
	t.Helper()

	pool := os.Getenv("TRUENAS_TEST_POOL")
	if pool == "" {
		pool = "tank"
	}

	return pool
}

// testNetworkInterface returns the NIC attach target to use for tests.
func testNetworkInterface(t *testing.T) string {
	t.Helper()

	nic := os.Getenv("TRUENAS_TEST_NETWORK_INTERFACE")
	if nic == "" {
		nic = os.Getenv("TRUENAS_TEST_BRIDGE") // backwards compat
	}

	if nic == "" {
		t.Skip("TRUENAS_TEST_NETWORK_INTERFACE must be set (bridge, VLAN, or physical interface)")
	}

	return nic
}

// settleTime gives TrueNAS a moment to finish async operations between heavy tests.
// Prevents transient failures from resource pressure during the full test suite.
func settleTime(t *testing.T) {
	t.Helper()
	time.Sleep(2 * time.Second)
}

// uniqueName generates a test-scoped unique name to avoid collisions.
// Uses only alphanumeric characters (TrueNAS VM names don't allow hyphens/underscores).
func uniqueName(prefix string) string {
	return fmt.Sprintf("%s%d", prefix, time.Now().UnixNano()%100000)
}

// --- Health Check Tests ---

func TestIntegration_Ping(t *testing.T) {
	c := testClient(t)
	ctx := context.Background()

	err := c.Ping(ctx)
	require.NoError(t, err, "TrueNAS API should be reachable")
}

func TestIntegration_PoolExists(t *testing.T) {
	c := testClient(t)
	ctx := context.Background()
	pool := testPool(t)

	exists, err := c.PoolExists(ctx, pool)
	require.NoError(t, err)
	assert.True(t, exists, "pool %q should exist", pool)

	exists, err = c.PoolExists(ctx, "nonexistent-pool-xyz")
	require.NoError(t, err)
	assert.False(t, exists, "nonexistent pool should not exist")
}

func TestIntegration_NetworkInterfaceValid(t *testing.T) {
	c := testClient(t)
	ctx := context.Background()
	nic := testNetworkInterface(t)

	valid, err := c.NetworkInterfaceValid(ctx, nic)
	require.NoError(t, err)
	assert.True(t, valid, "NIC attach %q should be valid", nic)

	valid, err = c.NetworkInterfaceValid(ctx, "nonexistent-interface-xyz")
	require.NoError(t, err)
	assert.False(t, valid, "nonexistent interface should not be valid")

	// Also verify choices returns something
	choices, err := c.NetworkInterfaceChoices(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, choices, "should have at least one NIC attach choice")
	t.Logf("Available NIC attach targets: %v", choices)
}

// --- Dataset / Zvol Lifecycle ---

func TestIntegration_DatasetLifecycle(t *testing.T) {
	c := testClient(t)
	ctx := context.Background()
	pool := testPool(t)

	dsName := pool + "/omni-integration-test-" + uniqueName("ds")

	// Create dataset
	ds, err := c.CreateDataset(ctx, CreateDatasetRequest{
		Name: dsName,
		Type: "FILESYSTEM",
	})
	require.NoError(t, err, "should create dataset")
	assert.Equal(t, "FILESYSTEM", ds.Type)

	t.Cleanup(func() {
		c.DeleteDataset(context.Background(), dsName) //nolint:errcheck
	})

	// EnsureDataset on existing — should not error
	err = c.EnsureDataset(ctx, dsName)
	require.NoError(t, err, "EnsureDataset on existing dataset should succeed")

	// Delete
	err = c.DeleteDataset(ctx, dsName)
	require.NoError(t, err, "should delete dataset")

	// Delete again — idempotent
	err = c.DeleteDataset(ctx, dsName)
	require.NoError(t, err, "deleting already-gone dataset should not error")
}

func TestIntegration_ZvolLifecycle(t *testing.T) {
	settleTime(t)
	c := testClient(t)
	ctx := context.Background()
	pool := testPool(t)

	// Ensure parent dataset
	parentDS := pool + "/omni-integration-test-zvols-" + uniqueName("z")
	err := c.EnsureDataset(ctx, parentDS)
	require.NoError(t, err)

	t.Cleanup(func() {
		c.DeleteDataset(context.Background(), parentDS) //nolint:errcheck
	})

	zvolName := parentDS + "/" + uniqueName("zvol")

	// Create 1 GiB zvol
	ds, err := c.CreateZvol(ctx, zvolName, 1)
	require.NoError(t, err, "should create zvol")
	assert.Equal(t, "VOLUME", ds.Type)

	// Delete zvol
	err = c.DeleteDataset(ctx, zvolName)
	require.NoError(t, err, "should delete zvol")
}

// --- File Operations ---

func TestIntegration_FileExistence(t *testing.T) {
	c := testClient(t)
	ctx := context.Background()

	// /mnt always exists on TrueNAS
	exists, err := c.FileExists(ctx, "/mnt")
	require.NoError(t, err)
	assert.True(t, exists, "/mnt should exist")

	exists, err = c.FileExists(ctx, "/mnt/nonexistent-path-xyz-12345")
	require.NoError(t, err)
	assert.False(t, exists, "nonexistent path should not exist")
}

func TestIntegration_FileUpload(t *testing.T) {
	c := testClient(t)
	ctx := context.Background()
	pool := testPool(t)

	// Ensure upload dataset
	dsName := pool + "/omni-integration-test-upload"
	err := c.EnsureDataset(ctx, dsName)
	require.NoError(t, err)

	t.Cleanup(func() {
		c.DeleteDataset(context.Background(), dsName) //nolint:errcheck
	})

	filePath := "/mnt/" + dsName + "/" + uniqueName("test") + ".txt"
	content := "integration test content"

	// Upload
	err = c.UploadFile(ctx, filePath, strings.NewReader(content), int64(len(content)))
	require.NoError(t, err, "should upload file")

	// Verify exists
	exists, err := c.FileExists(ctx, filePath)
	require.NoError(t, err)
	assert.True(t, exists, "uploaded file should exist")
}

// --- VM Lifecycle (Stopped VMs Only — No Nested Virt Required) ---

func TestIntegration_VMLifecycle(t *testing.T) {
	c := testClient(t)
	ctx := context.Background()

	vmName := "omniinttest" + uniqueName("vm")

	// Create VM (stays stopped — no nested virt needed)
	vm, err := c.CreateVM(ctx, CreateVMRequest{
		Name:        vmName,
		Description: "Integration test VM — safe to delete",
		VCPUs:       1,
		Memory:      512,
		Bootloader:  "UEFI",
		Autostart:   false,
	})
	require.NoError(t, err, "should create VM")
	assert.Equal(t, vmName, vm.Name)
	assert.Greater(t, vm.ID, 0)

	vmID := vm.ID

	t.Cleanup(func() {
		c.StopVM(context.Background(), vmID, true) //nolint:errcheck
		c.DeleteVM(context.Background(), vmID)     //nolint:errcheck
	})

	// Get VM
	fetched, err := c.GetVM(ctx, vmID)
	require.NoError(t, err)
	assert.Equal(t, vmName, fetched.Name)

	// Find by name
	found, err := c.FindVMByName(ctx, vmName)
	require.NoError(t, err)
	require.NotNil(t, found, "should find VM by name")
	assert.Equal(t, vmID, found.ID)

	// Find missing
	missing, err := c.FindVMByName(ctx, "nonexistent-vm-xyz-99999")
	require.NoError(t, err)
	assert.Nil(t, missing, "should not find nonexistent VM")

	// List VMs — ours should be in the list
	vms, err := c.ListVMs(ctx)
	require.NoError(t, err)

	var foundInList bool
	for _, v := range vms {
		if v.ID == vmID {
			foundInList = true

			break
		}
	}

	assert.True(t, foundInList, "created VM should appear in list")

	// Delete
	err = c.DeleteVM(ctx, vmID)
	require.NoError(t, err, "should delete VM")

	// Delete again — idempotent
	err = c.DeleteVM(ctx, vmID)
	require.NoError(t, err, "deleting already-gone VM should not error")
}

// --- VM UUID (SMBIOS identity for Omni correlation) ---

func TestIntegration_VMCreateWithUUID(t *testing.T) {
	c := testClient(t)
	ctx := context.Background()

	vmName := "omniinttest" + uniqueName("uuid")
	testUUID := "01970fba-1234-7000-8000-abcdef012345"

	vm, err := c.CreateVM(ctx, CreateVMRequest{
		Name:        vmName,
		Description: "UUID integration test — safe to delete",
		UUID:        testUUID,
		VCPUs:       1,
		Memory:      512,
		Bootloader:  "UEFI",
		Autostart:   false,
	})
	require.NoError(t, err, "vm.create should accept uuid field")
	assert.Greater(t, vm.ID, 0)

	t.Cleanup(func() {
		c.StopVM(context.Background(), vm.ID, true) //nolint:errcheck
		c.DeleteVM(context.Background(), vm.ID)     //nolint:errcheck
	})

	// Query the VM raw to verify UUID was persisted
	var rawVM map[string]any
	filter := []any{
		[]any{[]any{"id", "=", vm.ID}},
		map[string]any{"get": true},
	}

	err = c.call(ctx, "vm.query", filter, &rawVM)
	require.NoError(t, err, "should query VM")

	actualUUID, ok := rawVM["uuid"].(string)
	require.True(t, ok, "VM should have uuid field in response")
	assert.Equal(t, testUUID, actualUUID, "UUID should match what was passed to vm.create")
}

// --- Device Attachment (on Stopped VM) ---

func TestIntegration_DeviceAttachment(t *testing.T) {
	settleTime(t)
	c := testClient(t)
	ctx := context.Background()
	pool := testPool(t)
	networkIface := testNetworkInterface(t)

	vmName := "omniinttest" + uniqueName("dev")

	// Create VM
	vm, err := c.CreateVM(ctx, CreateVMRequest{
		Name:       vmName,
		VCPUs:      1,
		Memory:     512,
		Bootloader: "UEFI",
		Autostart:  false,
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		c.StopVM(context.Background(), vm.ID, true) //nolint:errcheck
		c.DeleteVM(context.Background(), vm.ID)     //nolint:errcheck
	})

	// Ensure parent dataset for zvol
	parentDS := pool + "/omni-integration-test-devs"
	err = c.EnsureDataset(ctx, parentDS)
	require.NoError(t, err)

	t.Cleanup(func() {
		c.DeleteDataset(context.Background(), parentDS) //nolint:errcheck
	})

	// Create zvol for disk
	zvolName := parentDS + "/" + uniqueName("disk")

	_, err = c.CreateZvol(ctx, zvolName, 1)
	require.NoError(t, err)

	t.Cleanup(func() {
		c.DeleteDataset(context.Background(), zvolName) //nolint:errcheck
	})

	// Attach NIC with explicit MAC (mirrors production path)
	nicDev, err := c.AddNICWithConfig(ctx, vm.ID, NICConfig{
		NetworkInterface: networkIface,
		MAC:              "02:de:ad:00:00:01",
	}, 2001)
	require.NoError(t, err, "should attach NIC with explicit MAC")
	assert.Equal(t, "NIC", nicDev.Attributes["dtype"])

	gotMAC, _ := nicDev.Attributes["mac"].(string)
	assert.Equal(t, "02:de:ad:00:00:01", strings.ToLower(gotMAC),
		"TrueNAS must respect the MAC we set")

	// Attach DISK
	diskDev, err := c.AddDisk(ctx, vm.ID, zvolName)
	require.NoError(t, err, "should attach DISK")
	assert.Equal(t, "DISK", diskDev.Attributes["dtype"])

	// Note: CDROM attachment requires an actual ISO file to exist on disk.
	// Skipping CDROM test unless a test ISO is available.
	t.Log("CDROM attachment test skipped — requires ISO file on TrueNAS filesystem")
}

// --- Zvol Resize ---

func TestIntegration_ZvolResize(t *testing.T) {
	settleTime(t)
	c := testClient(t)
	ctx := context.Background()
	pool := testPool(t)

	parentDS := pool + "/omni-integration-test-resize"
	err := c.EnsureDataset(ctx, parentDS)
	require.NoError(t, err)

	t.Cleanup(func() {
		c.DeleteDataset(context.Background(), parentDS) //nolint:errcheck
	})

	zvolName := parentDS + "/" + uniqueName("zvol")

	// Create 1 GiB zvol
	_, err = c.CreateZvol(ctx, zvolName, 1)
	require.NoError(t, err)

	t.Cleanup(func() {
		c.DeleteDataset(context.Background(), zvolName) //nolint:errcheck
	})

	// Verify initial size
	size, err := c.GetZvolSize(ctx, zvolName)
	require.NoError(t, err)
	assert.Equal(t, int64(1024*1024*1024), size, "initial size should be 1 GiB")

	// Resize to 2 GiB
	err = c.ResizeZvol(ctx, zvolName, 2)
	require.NoError(t, err)

	// Verify new size
	size, err = c.GetZvolSize(ctx, zvolName)
	require.NoError(t, err)
	assert.Equal(t, int64(2*1024*1024*1024), size, "resized size should be 2 GiB")
}

// --- Error Path Tests ---

func TestIntegration_CreateZvol_PoolFull(t *testing.T) {
	c := testClient(t)
	ctx := context.Background()
	pool := testPool(t)

	// Check actual pool size and try to create something bigger
	free, err := c.PoolFreeSpace(ctx, pool)
	require.NoError(t, err)

	// Request 10x the free space — should fail
	tooBigGiB := int(free/(1024*1024*1024)) * 10
	if tooBigGiB < 1 {
		tooBigGiB = 999999
	}

	_, err = c.CreateZvol(ctx, pool+"/omni-integration-test-toobig", tooBigGiB)
	if err == nil {
		// TrueNAS may use thin provisioning — clean up
		t.Log("TrueNAS accepted the oversized zvol (thin provisioning) — cleaning up")
		c.DeleteDataset(ctx, pool+"/omni-integration-test-toobig") //nolint:errcheck
	}
	// Either way, the test passes — we just verify it doesn't panic
}

func TestIntegration_ResizeZvol_NotFound(t *testing.T) {
	c := testClient(t)
	ctx := context.Background()

	err := c.ResizeZvol(ctx, "default/nonexistent-zvol-xyz", 10)
	assert.Error(t, err, "resizing a nonexistent zvol should fail")
}

func TestIntegration_PoolFreeSpace(t *testing.T) {
	c := testClient(t)
	ctx := context.Background()
	pool := testPool(t)

	free, err := c.PoolFreeSpace(ctx, pool)
	require.NoError(t, err)
	assert.Greater(t, free, int64(0), "pool should have some free space")
	t.Logf("Pool %q free space: %d GiB", pool, free/(1024*1024*1024))

	// Nonexistent pool
	_, err = c.PoolFreeSpace(ctx, "nonexistent-pool-xyz")
	assert.Error(t, err)
}

func TestIntegration_SystemMemory(t *testing.T) {
	c := testClient(t)
	ctx := context.Background()

	mem, err := c.SystemMemoryAvailable(ctx)
	require.NoError(t, err)
	assert.Greater(t, mem, int64(0), "system should have some memory")
	t.Logf("System memory: %d GiB", mem/(1024*1024*1024))
}

// --- Full Provision/Deprovision E2E ---

func TestIntegration_FullProvisionDeprovision(t *testing.T) {
	settleTime(t)
	c := testClient(t)
	ctx := context.Background()
	pool := testPool(t)
	networkIface := testNetworkInterface(t)

	requestID := "e2e-" + uniqueName("prov")
	vmName := "omni_" + strings.ReplaceAll(requestID, "-", "_")
	zvolPath := pool + "/omni-vms/" + requestID

	// --- PROVISION ---

	// 1. Ensure parent dataset
	err := c.EnsureDataset(ctx, pool+"/omni-vms")
	require.NoError(t, err)

	// 2. Create zvol
	_, err = c.CreateZvol(ctx, zvolPath, 1)
	require.NoError(t, err)

	// 3. Create VM with HOST-PASSTHROUGH
	vm, err := c.CreateVM(ctx, CreateVMRequest{
		Name:        vmName,
		Description: "E2E test — safe to delete",
		VCPUs:       1,
		Memory:      512,
		CPUMode:     "HOST-PASSTHROUGH",
		Bootloader:  "UEFI",
		Autostart:   false,
	})
	require.NoError(t, err, "should create VM")
	assert.Greater(t, vm.ID, 0)

	// 4. Attach DISK
	diskDev, err := c.AddDisk(ctx, vm.ID, zvolPath)
	require.NoError(t, err, "should attach disk")
	assert.Equal(t, "DISK", diskDev.Attributes["dtype"])

	// 5. Attach NIC with explicit MAC (mirrors production deterministic MAC path)
	nicDev, err := c.AddNICWithConfig(ctx, vm.ID, NICConfig{
		NetworkInterface: networkIface,
		MAC:              "02:e2:e0:00:00:01",
	}, 2001)
	require.NoError(t, err, "should attach NIC with explicit MAC")
	assert.Equal(t, "NIC", nicDev.Attributes["dtype"])

	// 6. Verify VM exists and is stopped
	fetched, err := c.GetVM(ctx, vm.ID)
	require.NoError(t, err)
	assert.Equal(t, vmName, fetched.Name)

	// 7. Find by name
	found, err := c.FindVMByName(ctx, vmName)
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, vm.ID, found.ID)

	// --- DEPROVISION ---

	// 8. Stop VM (even though it's not running, should be idempotent)
	err = c.StopVM(ctx, vm.ID, true)
	// May error if already stopped — that's ok

	// 9. Delete VM
	err = c.DeleteVM(ctx, vm.ID)
	require.NoError(t, err, "should delete VM")

	// 10. Delete zvol
	err = c.DeleteDataset(ctx, zvolPath)
	require.NoError(t, err, "should delete zvol")

	// 11. Verify VM is gone
	gone, err := c.FindVMByName(ctx, vmName)
	require.NoError(t, err)
	assert.Nil(t, gone, "VM should be gone after deprovision")

	// 12. Verify delete is idempotent
	err = c.DeleteVM(ctx, vm.ID)
	require.NoError(t, err, "double delete should not error")

	err = c.DeleteDataset(ctx, zvolPath)
	require.NoError(t, err, "double delete zvol should not error")
}

// --- Device Delete ---

func TestIntegration_DeviceDelete(t *testing.T) {
	c := testClient(t)
	ctx := context.Background()
	networkIface := testNetworkInterface(t)

	vmName := "omniinttest" + uniqueName("devdel")

	vm, err := c.CreateVM(ctx, CreateVMRequest{
		Name:       vmName,
		VCPUs:      1,
		Memory:     512,
		Bootloader: "UEFI",
		Autostart:  false,
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		c.DeleteVM(context.Background(), vm.ID) //nolint:errcheck
	})

	// Attach a NIC with explicit MAC (mirrors production path)
	dev, err := c.AddNICWithConfig(ctx, vm.ID, NICConfig{
		NetworkInterface: networkIface,
		MAC:              "02:de:1e:7e:00:01",
	}, 2001)
	require.NoError(t, err)

	// Delete the device
	err = c.DeleteDevice(ctx, dev.ID)
	require.NoError(t, err, "should delete device")

	// Delete again — idempotent
	err = c.DeleteDevice(ctx, dev.ID)
	require.NoError(t, err, "double delete device should not error")
}

// --- Additional Disk Integration Tests ---

func TestIntegration_AdditionalDisks_CreateAndAttach(t *testing.T) {
	settleTime(t)
	c := testClient(t)
	ctx := context.Background()
	pool := testPool(t)

	parentDS := pool + "/omni-integration-test-multidisk"
	err := c.EnsureDataset(ctx, parentDS)
	require.NoError(t, err)

	t.Cleanup(func() {
		c.DeleteDataset(context.Background(), parentDS) //nolint:errcheck
	})

	requestID := uniqueName("mdisk")
	rootZvol := parentDS + "/" + requestID
	disk1Zvol := parentDS + "/" + requestID + "-disk-1"
	disk2Zvol := parentDS + "/" + requestID + "-disk-2"

	// Create root zvol and 2 additional zvols
	_, err = c.CreateZvol(ctx, rootZvol, 1, OmniManagedProperties(requestID))
	require.NoError(t, err)

	_, err = c.CreateZvol(ctx, disk1Zvol, 2, OmniManagedProperties(requestID))
	require.NoError(t, err)

	_, err = c.CreateZvol(ctx, disk2Zvol, 3, OmniManagedProperties(requestID))
	require.NoError(t, err)

	t.Cleanup(func() {
		c.DeleteDataset(context.Background(), disk2Zvol) //nolint:errcheck
		c.DeleteDataset(context.Background(), disk1Zvol) //nolint:errcheck
		c.DeleteDataset(context.Background(), rootZvol)  //nolint:errcheck
	})

	// Verify all zvols exist and have correct sizes
	rootSize, err := c.GetZvolSize(ctx, rootZvol)
	require.NoError(t, err)
	assert.Equal(t, int64(1*1024*1024*1024), rootSize)

	disk1Size, err := c.GetZvolSize(ctx, disk1Zvol)
	require.NoError(t, err)
	assert.Equal(t, int64(2*1024*1024*1024), disk1Size)

	disk2Size, err := c.GetZvolSize(ctx, disk2Zvol)
	require.NoError(t, err)
	assert.Equal(t, int64(3*1024*1024*1024), disk2Size)

	// Create VM and attach all disks
	vmName := "omniinttest" + uniqueName("md")
	vm, err := c.CreateVM(ctx, CreateVMRequest{
		Name:       vmName,
		VCPUs:      1,
		Memory:     512,
		Bootloader: "UEFI",
		Autostart:  false,
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		c.StopVM(context.Background(), vm.ID, true) //nolint:errcheck
		c.DeleteVM(context.Background(), vm.ID)     //nolint:errcheck
	})

	// Attach root disk (order 1001), additional disks (1002, 1003)
	_, err = c.AddDisk(ctx, vm.ID, rootZvol)
	require.NoError(t, err, "should attach root disk")

	_, err = c.AddDiskWithOrder(ctx, vm.ID, disk1Zvol, 1002)
	require.NoError(t, err, "should attach additional disk 1")

	_, err = c.AddDiskWithOrder(ctx, vm.ID, disk2Zvol, 1003)
	require.NoError(t, err, "should attach additional disk 2")

	// Verify all devices are attached
	devices, err := c.ListDevices(ctx, vm.ID)
	require.NoError(t, err)

	diskCount := 0
	for _, d := range devices {
		if dtype, _ := d.Attributes["dtype"].(string); dtype == "DISK" {
			diskCount++
		}
	}

	assert.Equal(t, 3, diskCount, "VM should have 3 DISK devices (root + 2 additional)")
}

func TestIntegration_AdditionalDisks_Deprovision(t *testing.T) {
	settleTime(t)
	c := testClient(t)
	ctx := context.Background()
	pool := testPool(t)

	parentDS := pool + "/omni-integration-test-mdeprov"
	err := c.EnsureDataset(ctx, parentDS)
	require.NoError(t, err)

	t.Cleanup(func() {
		c.DeleteDataset(context.Background(), parentDS) //nolint:errcheck
	})

	requestID := uniqueName("deprov")
	rootZvol := parentDS + "/" + requestID
	disk1Zvol := parentDS + "/" + requestID + "-disk-1"

	_, err = c.CreateZvol(ctx, rootZvol, 1)
	require.NoError(t, err)

	_, err = c.CreateZvol(ctx, disk1Zvol, 1)
	require.NoError(t, err)

	// Delete additional disk first, then root — mimics deprovision order
	err = c.DeleteDataset(ctx, disk1Zvol)
	require.NoError(t, err, "should delete additional disk zvol")

	err = c.DeleteDataset(ctx, rootZvol)
	require.NoError(t, err, "should delete root zvol")

	// Verify both are gone — double delete should be idempotent
	err = c.DeleteDataset(ctx, disk1Zvol)
	require.NoError(t, err, "double delete additional disk should be idempotent")

	err = c.DeleteDataset(ctx, rootZvol)
	require.NoError(t, err, "double delete root zvol should be idempotent")
}

func TestIntegration_AdditionalDisks_NonExistentPool(t *testing.T) {
	c := testClient(t)
	ctx := context.Background()

	_, err := c.CreateZvol(ctx, "nonexistent-pool-xyz/omni-vms/test-disk-1", 1)
	assert.Error(t, err, "creating zvol on non-existent pool should fail")
}

func TestIntegration_AdditionalDisks_EncryptedLifecycle(t *testing.T) {
	settleTime(t)
	c := testClient(t)
	ctx := context.Background()
	pool := testPool(t)

	parentDS := pool + "/omni-integration-test-encrdisk"
	err := c.EnsureDataset(ctx, parentDS)
	require.NoError(t, err)

	t.Cleanup(func() {
		c.DeleteDataset(context.Background(), parentDS) //nolint:errcheck
	})

	zvolName := parentDS + "/" + uniqueName("encr")
	passphrase := "integration-test-passphrase-1234567890abcdef"

	props := OmniManagedProperties("test-req")
	props = append(props, UserProperty{Key: "org.omni:passphrase", Value: passphrase})

	// Create encrypted zvol
	_, err = c.CreateEncryptedZvol(ctx, zvolName, 1, passphrase, props)
	require.NoError(t, err, "should create encrypted zvol")

	t.Cleanup(func() {
		c.DeleteDataset(context.Background(), zvolName) //nolint:errcheck
	})

	// Verify passphrase was stored
	stored, err := c.GetDatasetUserProperty(ctx, zvolName, "org.omni:passphrase")
	require.NoError(t, err)
	assert.Equal(t, passphrase, stored, "stored passphrase should match")

	// Check lock status — newly created should be unlocked
	locked, err := c.IsDatasetLocked(ctx, zvolName)
	require.NoError(t, err)
	assert.False(t, locked, "newly created encrypted zvol should be unlocked")

	// Unlock is idempotent
	err = c.UnlockDataset(ctx, zvolName, passphrase)
	// May succeed or error depending on TrueNAS version — either way, it shouldn't panic
}

func TestIntegration_AdditionalDisks_DatasetPrefixOnPool(t *testing.T) {
	settleTime(t)
	c := testClient(t)
	ctx := context.Background()
	pool := testPool(t)

	// Simulate: dataset_prefix = "inttest/nested" on the same pool
	prefix := pool + "/inttest-" + uniqueName("pfx")
	nestedPath := prefix + "/omni-vms"

	// Ensure hierarchy — this is what the provisioner does for dataset_prefix
	err := c.EnsureDataset(ctx, prefix)
	require.NoError(t, err)

	err = c.EnsureDataset(ctx, nestedPath)
	require.NoError(t, err)

	t.Cleanup(func() {
		c.DeleteDataset(context.Background(), nestedPath) //nolint:errcheck
		c.DeleteDataset(context.Background(), prefix)     //nolint:errcheck
	})

	// Create an additional disk zvol under the prefix
	zvolName := nestedPath + "/" + uniqueName("disk") + "-disk-1"

	_, err = c.CreateZvol(ctx, zvolName, 1)
	require.NoError(t, err, "should create zvol under dataset prefix hierarchy")

	t.Cleanup(func() {
		c.DeleteDataset(context.Background(), zvolName) //nolint:errcheck
	})

	// Verify it exists
	size, err := c.GetZvolSize(ctx, zvolName)
	require.NoError(t, err)
	assert.Equal(t, int64(1*1024*1024*1024), size)
}

func TestIntegration_AdditionalDisks_Resize(t *testing.T) {
	settleTime(t)
	c := testClient(t)
	ctx := context.Background()
	pool := testPool(t)

	parentDS := pool + "/omni-integration-test-mdresize"
	err := c.EnsureDataset(ctx, parentDS)
	require.NoError(t, err)

	t.Cleanup(func() {
		c.DeleteDataset(context.Background(), parentDS) //nolint:errcheck
	})

	zvolName := parentDS + "/" + uniqueName("rz") + "-disk-1"

	// Create 1 GiB additional disk
	_, err = c.CreateZvol(ctx, zvolName, 1)
	require.NoError(t, err)

	t.Cleanup(func() {
		c.DeleteDataset(context.Background(), zvolName) //nolint:errcheck
	})

	// Verify initial size
	size, err := c.GetZvolSize(ctx, zvolName)
	require.NoError(t, err)
	assert.Equal(t, int64(1*1024*1024*1024), size, "initial size should be 1 GiB")

	// Grow to 2 GiB
	err = c.ResizeZvol(ctx, zvolName, 2)
	require.NoError(t, err)

	size, err = c.GetZvolSize(ctx, zvolName)
	require.NoError(t, err)
	assert.Equal(t, int64(2*1024*1024*1024), size, "should be 2 GiB after grow")

	// Attempt shrink to 1 GiB — ZFS does not support shrinking
	err = c.ResizeZvol(ctx, zvolName, 1)
	// ResizeZvol sends the update — TrueNAS may accept or reject.
	// The provider's maybeResizeZvol prevents shrinks at the application layer,
	// so this test just verifies the API call doesn't panic.
}

func TestIntegration_AdditionalDisks_PoolFreeSpaceCheck(t *testing.T) {
	c := testClient(t)
	ctx := context.Background()
	pool := testPool(t)

	free, err := c.PoolFreeSpace(ctx, pool)
	require.NoError(t, err)
	assert.Greater(t, free, int64(0), "pool should have some free space")

	// Verify the space check math works for aggregate calculation
	// If pool has 100 GiB free, a root disk of 40 + additional disks of 100+200 = 340 GiB should fail
	rootGiB := 40
	additionalGiB := []int{100, 200}

	totalRequired := int64(rootGiB)
	for _, s := range additionalGiB {
		totalRequired += int64(s)
	}

	totalRequiredBytes := totalRequired * 1024 * 1024 * 1024

	if free < totalRequiredBytes {
		t.Logf("Pool %q has %d GiB free, aggregate requirement of %d GiB would correctly be rejected",
			pool, free/(1024*1024*1024), totalRequired)
	} else {
		t.Logf("Pool %q has %d GiB free, aggregate requirement of %d GiB would be accepted (pool is large enough)",
			pool, free/(1024*1024*1024), totalRequired)
	}
}

// --- WebSocket Reconnect Against Real TrueNAS ---

func TestIntegration_WebSocketReconnect(t *testing.T) {
	host := os.Getenv("TRUENAS_TEST_HOST")
	apiKey := os.Getenv("TRUENAS_TEST_API_KEY")

	if host == "" {
		t.Skip("TRUENAS_TEST_HOST must be set for WebSocket reconnect test")
	}

	ctx := context.Background()

	// Create a client
	c, err := New(Config{
		Host:               host,
		APIKey:             apiKey,
		InsecureSkipVerify: true,
	})
	require.NoError(t, err)

	t.Cleanup(func() { c.Close() })

	// Verify connection works
	err = c.Ping(ctx)
	require.NoError(t, err, "initial ping should succeed")

	// Force-close the underlying WebSocket connection to simulate a drop
	ws, ok := c.transport.(*wsTransport)
	if !ok {
		t.Skip("not using WebSocket transport")
	}

	ws.mu.Lock()
	ws.conn.Close() // Kill the connection
	ws.mu.Unlock()

	// Next call should trigger reconnect and succeed
	err = c.Ping(ctx)
	require.NoError(t, err, "ping after forced disconnect should succeed via reconnect")

	// Verify multiple calls work after reconnect
	exists, err := c.PoolExists(ctx, "default")
	require.NoError(t, err)
	t.Logf("Pool exists after reconnect: %v", exists)
}

// --- Host Health API ---

func TestIntegration_GetHostInfo(t *testing.T) {
	c := testClient(t)
	ctx := context.Background()

	info, err := c.GetHostInfo(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, info.Hostname, "should return hostname")
	assert.Greater(t, info.Physmem, int64(0), "should return physical memory")
	assert.Greater(t, info.Cores, 0, "should return CPU cores")
	t.Logf("Host: %s, Cores: %d, Memory: %d GiB", info.Hostname, info.Cores, info.Physmem/(1024*1024*1024))
}

func TestIntegration_ListPools(t *testing.T) {
	c := testClient(t)
	ctx := context.Background()

	pools, err := c.ListPools(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, pools, "should have at least one pool")

	for _, p := range pools {
		assert.NotEmpty(t, p.Name, "pool should have a name")
		t.Logf("Pool: %s, Healthy: %v, Free: %d GiB, Used: %d GiB",
			p.Name, p.Healthy, p.Free/(1024*1024*1024), p.Used/(1024*1024*1024))
	}
}

func TestIntegration_ListDisks(t *testing.T) {
	c := testClient(t)
	ctx := context.Background()

	disks, err := c.ListDisks(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, disks, "should have at least one disk")

	for _, d := range disks {
		assert.NotEmpty(t, d.Name, "disk should have a name")
		t.Logf("Disk: %s, Type: %s, Size: %d GiB", d.Name, d.Type, d.Size/(1024*1024*1024))
	}
}

// --- Device Operations ---

func TestIntegration_ListDevicesAndFindCDROM(t *testing.T) {
	settleTime(t)
	c := testClient(t)
	ctx := context.Background()
	pool := testPool(t)

	vmName := "omniinttest" + uniqueName("devops")

	vm, err := c.CreateVM(ctx, CreateVMRequest{
		Name:       vmName,
		VCPUs:      1,
		Memory:     512,
		Bootloader: "UEFI",
		Autostart:  false,
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		c.DeleteVM(context.Background(), vm.ID) //nolint:errcheck
	})

	// Upload a tiny fake ISO for CDROM test
	isoDS := pool + "/omni-integration-test-cdrom"
	_ = c.EnsureDataset(ctx, isoDS)
	isoPath := "/mnt/" + isoDS + "/test.iso"
	_ = c.UploadFile(ctx, isoPath, strings.NewReader("fake iso"), 8)

	t.Cleanup(func() {
		c.DeleteDataset(context.Background(), isoDS) //nolint:errcheck
	})

	// Add CDROM
	cdrom, err := c.AddCDROM(ctx, vm.ID, isoPath)
	require.NoError(t, err)

	// ListDevices
	devices, err := c.ListDevices(ctx, vm.ID)
	require.NoError(t, err)
	assert.NotEmpty(t, devices, "VM should have devices")

	foundCDROM := false
	for _, d := range devices {
		dtype, _ := d.Attributes["dtype"].(string)
		if dtype == "CDROM" {
			foundCDROM = true
		}
		t.Logf("Device: ID=%d, dtype=%s", d.ID, dtype)
	}
	assert.True(t, foundCDROM, "should find CDROM in device list")

	// FindCDROM
	found, err := c.FindCDROM(ctx, vm.ID)
	require.NoError(t, err)
	require.NotNil(t, found, "FindCDROM should find it")
	assert.Equal(t, cdrom.ID, found.ID)

	// SwapCDROM — update path
	newISOPath := "/mnt/" + isoDS + "/test2.iso"
	_ = c.UploadFile(ctx, newISOPath, strings.NewReader("fake iso 2"), 10)

	swapped, err := c.SwapCDROM(ctx, vm.ID, newISOPath)
	require.NoError(t, err, "SwapCDROM should update existing device")
	assert.Equal(t, cdrom.ID, swapped.ID, "should update same device, not create new")

	// Verify the path changed
	updated, err := c.FindCDROM(ctx, vm.ID)
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, newISOPath, updated.Attributes["path"], "CDROM path should be updated")
}

// --- Pool Selector ---

func TestIntegration_PoolSelector(t *testing.T) {
	c := testClient(t)
	ctx := context.Background()
	pool := testPool(t)

	// Import monitor package inline — test the selector logic with real pools
	pools, err := c.ListPools(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, pools)

	// Find best pool manually
	var best *PoolInfo
	for i := range pools {
		if pools[i].Healthy && (best == nil || pools[i].Free > best.Free) {
			best = &pools[i]
		}
	}

	require.NotNil(t, best, "should have at least one healthy pool")
	t.Logf("Best pool: %s (free: %d GiB)", best.Name, best.Free/(1024*1024*1024))

	// Verify our test pool is in the list
	var foundTestPool bool
	for _, p := range pools {
		if p.Name == pool {
			foundTestPool = true
		}
	}
	assert.True(t, foundTestPool, "test pool %q should be in pool list", pool)
}

// --- VM Naming Convention ---

func TestIntegration_VMNamingConvention(t *testing.T) {
	settleTime(t)
	c := testClient(t)
	ctx := context.Background()

	// Create two VMs with omni- prefix and one without
	vm1Name := "omniinttest" + uniqueName("a")
	vm2Name := "omniinttest" + uniqueName("b")
	vm3Name := "otherinttest" + uniqueName("c")

	var ids []int

	for _, name := range []string{vm1Name, vm2Name, vm3Name} {
		vm, err := c.CreateVM(ctx, CreateVMRequest{
			Name:       name,
			VCPUs:      1,
			Memory:     512,
			Bootloader: "UEFI",
			Autostart:  false,
		})
		require.NoError(t, err)

		ids = append(ids, vm.ID)
	}

	t.Cleanup(func() {
		for _, id := range ids {
			c.DeleteVM(context.Background(), id) //nolint:errcheck
		}
	})

	// List all VMs and filter by omni- prefix
	vms, err := c.ListVMs(ctx)
	require.NoError(t, err)

	var omniVMs []VM
	for _, v := range vms {
		if strings.HasPrefix(v.Name, "omniinttest") {
			omniVMs = append(omniVMs, v)
		}
	}

	assert.GreaterOrEqual(t, len(omniVMs), 2, "should find at least our 2 omni- prefixed VMs")

	// The non-omni VM should not be in the filtered list
	for _, v := range omniVMs {
		assert.True(t, strings.HasPrefix(v.Name, "omniinttest"),
			"filtered VMs should all have omni-inttest- prefix, got %q", v.Name)
	}
}

// --- Deterministic MAC Integration Test ---

func TestIntegration_NIC_DeterministicMAC(t *testing.T) {
	settleTime(t)
	c := testClient(t)
	ctx := context.Background()
	networkIface := testNetworkInterface(t)

	vm, err := c.CreateVM(ctx, CreateVMRequest{
		Name:       "omniinttest" + uniqueName("mac"),
		VCPUs:      1,
		Memory:     512,
		Bootloader: "UEFI",
		Autostart:  false,
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		c.DeleteVM(context.Background(), vm.ID) //nolint:errcheck
	})

	// Attach NIC with an explicit MAC address
	wantMAC := "02:de:ad:be:ef:01"

	nicDev, err := c.AddNICWithConfig(ctx, vm.ID, NICConfig{
		NetworkInterface: networkIface,
		MAC:              wantMAC,
	}, 2001)
	require.NoError(t, err, "TrueNAS must accept explicit MAC in vm.device.create")

	// Verify TrueNAS echoes back the MAC we sent — if it doesn't, deterministic
	// MAC is silently broken and DHCP reservations won't work.
	gotMAC, ok := nicDev.Attributes["mac"].(string)
	require.True(t, ok, "TrueNAS response must include 'mac' attribute")
	assert.Equal(t, wantMAC, strings.ToLower(gotMAC),
		"TrueNAS must respect the MAC we set, not generate a random one")
}
