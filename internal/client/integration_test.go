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

// testNICAttach returns the NIC attach target to use for tests.
func testNICAttach(t *testing.T) string {
	t.Helper()

	nic := os.Getenv("TRUENAS_TEST_NIC_ATTACH")
	if nic == "" {
		nic = os.Getenv("TRUENAS_TEST_BRIDGE") // backwards compat
	}

	if nic == "" {
		t.Skip("TRUENAS_TEST_NIC_ATTACH must be set (bridge, VLAN, or physical interface)")
	}

	return nic
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

func TestIntegration_NICAttachValid(t *testing.T) {
	c := testClient(t)
	ctx := context.Background()
	nic := testNICAttach(t)

	valid, err := c.NICAttachValid(ctx, nic)
	require.NoError(t, err)
	assert.True(t, valid, "NIC attach %q should be valid", nic)

	valid, err = c.NICAttachValid(ctx, "nonexistent-interface-xyz")
	require.NoError(t, err)
	assert.False(t, valid, "nonexistent interface should not be valid")

	// Also verify choices returns something
	choices, err := c.NICAttachChoices(ctx)
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
	c := testClient(t)
	ctx := context.Background()
	pool := testPool(t)

	// Ensure parent dataset
	parentDS := pool + "/omni-integration-test-zvols"
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

// --- Device Attachment (on Stopped VM) ---

func TestIntegration_DeviceAttachment(t *testing.T) {
	c := testClient(t)
	ctx := context.Background()
	pool := testPool(t)
	nicAttach := testNICAttach(t)

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

	// Attach NIC
	nicDev, err := c.AddNIC(ctx, vm.ID, nicAttach)
	require.NoError(t, err, "should attach NIC")
	assert.Equal(t, "NIC", nicDev.Attributes["dtype"])

	// Attach DISK
	diskDev, err := c.AddDisk(ctx, vm.ID, zvolName)
	require.NoError(t, err, "should attach DISK")
	assert.Equal(t, "DISK", diskDev.Attributes["dtype"])

	// Note: CDROM attachment requires an actual ISO file to exist on disk.
	// Skipping CDROM test unless a test ISO is available.
	t.Log("CDROM attachment test skipped — requires ISO file on TrueNAS filesystem")
}

// --- VM Naming Convention ---

func TestIntegration_VMNamingConvention(t *testing.T) {
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
