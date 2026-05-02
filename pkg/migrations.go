package pkg

import _ "embed"

//go:embed .../migrations/001_initial_schema.sql
var schema1 string

//go:embed migrations/002_add_user_id.sql
var schema_userid string

//go:embed migrations/002_fleet_console.sql
var schema2 string

//go:embed migrations/003_security_group_rules.sql
var schema3 string

//go:embed migrations/004_storage_enhancements.sql
var schema4 string

//go:embed migrations/005_update_volume_fields.sql
var schema5 string

//go:embed migrations/006_add_instance_tags.sql
var schema6 string
//go:embed migrations/007_add_volume_id_to_snapshots.sql
var schema7 string
//go:embed migrations/008_add_volume_tags.sql
var schema8 string
//go:embed migrations/009_add_size_to_snapshots.sql
var schema9 string
//go:embed migrations/010_make_template_instance_id_optional.sql
var schema10 string
//go:embed migrations/011_add_template_config_fields.sql
var schema11 string

//go:embed migrations/012_add_vpc_id_to_instances.sql
var schema12 string

var Schema = schema1 + "\n" + schema_userid + "\n" + schema2 + "\n" + schema3 + "\n" + schema4 + "\n" + schema5 + "\n" + schema6 + "\n" + schema7 + "\n" + schema8 + "\n" + schema9 + "\n" + schema10 + "\n" + schema11 + "\n" + schema12
