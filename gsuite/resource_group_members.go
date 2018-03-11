package gsuite

import (
	"fmt"
	"log"

	"github.com/hashicorp/terraform/helper/schema"
	directory "google.golang.org/api/admin/directory/v1"
)

var rolesMap = map[string]string{
	"MANAGER": "managers",
	"MEMBER":  "members",
	"OWNER":   "owners",
}

func resourceGroupMembers() *schema.Resource {
	return &schema.Resource{
		Create: resourceGroupMembersCreate,
		Read:   resourceGroupMembersRead,
		Update: resourceGroupMembersUpdate,
		Delete: resourceGroupMembersDelete,

		Schema: map[string]*schema.Schema{
			"group": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
			},
			"owners": {
				Type:     schema.TypeSet,
				Required: true,
				Elem:     &schema.Schema{Type: schema.TypeString},
				Set:      schema.HashString,
			},
			"managers": {
				Type:     schema.TypeSet,
				Required: true,
				Elem:     &schema.Schema{Type: schema.TypeString},
				Set:      schema.HashString,
			},
			"members": {
				Type:     schema.TypeSet,
				Required: true,
				Elem:     &schema.Schema{Type: schema.TypeString},
				Set:      schema.HashString,
			},
		},
	}
}

func resourceGroupMembersCreate(d *schema.ResourceData, meta interface{}) error {
	config := meta.(*Config)
	gid := d.Get("group").(string)

	for role := range rolesMap {
		// Get members from config
		cfgMembers := resourceRoleMembers(d, rolesMap[role])

		// Get members from API
		apiMembers, err := getApiMembers(gid, role, config)
		if err != nil {
			return fmt.Errorf("Error adding members: %v", err)
		}

		// This call removes any members that aren't defined in cfgMembers,
		// and adds all of those that are
		err = reconcileMembers(cfgMembers, apiMembers, config, gid, role)
		if err != nil {
			return fmt.Errorf("Error adding members: %v", err)
		}
	}

	d.SetId(gid)
	return resourceGroupMembersRead(d, meta)
}

func resourceGroupMembersRead(d *schema.ResourceData, meta interface{}) error {
	config := meta.(*Config)

	for role := range rolesMap {
		roleMembers, err := getApiMembers(d.Id(), role, config)
		if err != nil {
			return err
		}
		d.Set(rolesMap[role], roleMembers)
	}

	d.Set("group", d.Id())
	return nil
}

func resourceGroupMembersUpdate(d *schema.ResourceData, meta interface{}) error {
	log.Printf("[DEBUG]: Updating gsuite_group_members")
	config := meta.(*Config)
	gid := d.Get("group").(string)

	for role := range rolesMap {
		// Get members from config
		cfgMembers := resourceRoleMembers(d, rolesMap[role])

		// Get members from API
		apiMembers, err := getApiMembers(gid, role, config)
		if err != nil {
			return fmt.Errorf("Error updating memberships: %v", err)
		}

		// This call removes any members that aren't defined in cfgMembers,
		// and adds all of those that are
		err = reconcileMembers(cfgMembers, apiMembers, config, gid, role)
		if err != nil {
			return fmt.Errorf("Error updating memberships: %v", err)
		}
	}

	return resourceGroupMembersRead(d, meta)
}

func resourceGroupMembersDelete(d *schema.ResourceData, meta interface{}) error {
	log.Printf("[DEBUG]: Deleting gsuite_group_members")
	config := meta.(*Config)

	for role := range rolesMap {
		roleMembers := resourceRoleMembers(d, rolesMap[role])
		for _, s := range roleMembers {
			deleteMember(s, d.Id(), config)
		}
	}
	d.SetId("")
	return nil
}

// This function ensures that the members of a group exactly match that
// in a config by disabling any services that are returned by the API but not present
// in the config
func reconcileMembers(cfgMembers, apiMembers []string, config *Config, gid, role string) error {
	// Helper to convert slice to map
	m := func(vals []string) map[string]struct{} {
		sm := make(map[string]struct{})
		for _, s := range vals {
			sm[s] = struct{}{}
		}
		return sm
	}

	cfgMap := m(cfgMembers)
	apiMap := m(apiMembers)

	for k, _ := range apiMap {
		if _, ok := cfgMap[k]; !ok {
			// The member in the API is not in the config; disable it.
			err := deleteMember(k, gid, config)
			if err != nil {
				return err
			}
		} else {
			// The member exists in the config and the API, so we don't need
			// to re-enable it
			delete(cfgMap, k)
		}
	}

	for k, _ := range cfgMap {
		err := addMember(k, gid, role, config)
		if err != nil {
			return err
		}
	}
	return nil
}

// Retrieve a group's members from the API
func getApiMembers(gid, role string, config *Config) ([]string, error) {
	apiMembers := make([]string, 0)
	// Get members from the API
	groupMembers, err := config.directory.Members.List(gid).Roles(role).Do()
	if err != nil {
		return nil, err
	}
	for _, member := range groupMembers.Members {
		if member.Role == role {
			apiMembers = append(apiMembers, member.Email)
		}
	}
	return apiMembers, nil
}

func addMember(m, gid, role string, config *Config) error {
	groupMember := &directory.Member{
		Role:  role,
		Email: m,
	}

	createdGroupMember, err := config.directory.Members.Insert(gid, groupMember).Do()
	if err != nil {
		return fmt.Errorf("Error creating groupMember: %s", err)
	}
	log.Printf("[INFO] Created group: %s", createdGroupMember.Email)
	return nil
}

func deleteMember(m, gid string, config *Config) error {
	err := config.directory.Members.Delete(gid, m).Do()
	if err != nil {
		return fmt.Errorf("Error deleting group: %s", err)
	}
	return nil
}

func resourceRoleMembers(d *schema.ResourceData, key string) []string {
	// Calculate the tags
	var members []string
	if s := d.Get(key); s != nil {
		ss := s.(*schema.Set)
		members = make([]string, ss.Len())
		for i, v := range ss.List() {
			members[i] = v.(string)
		}
	}
	return members
}
