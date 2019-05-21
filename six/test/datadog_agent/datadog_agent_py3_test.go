// +build three

package testdatadogagent

import "testing"

func TestSetExternalTagsUnicodeUnsuported(t *testing.T) {
	code := `
	tags = [
		('hostname', {'source_type': [u'tag1', 123, u'tag2\u00E1']}),
		('hostname2', {'source_type2': [u'tag3', [], u'tag4']}),
		('hostname', {'source_type': [1,2,3]}),
	]
	datadog_agent.set_external_tags(tags)
	`
	out, err := run(code)
	if err != nil {
		t.Fatal(err)
	}
	if out != "hostname,source_type,tag1,tag2\u00E1\nhostname2,source_type2,tag3,tag4" {
		t.Errorf("Unexpected printed value: '%s'", out)
	}
}
