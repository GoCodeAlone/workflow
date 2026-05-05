package interfaces

import "testing"

func TestValidateUUID(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"canonical lowercase", "f8b6200c-3bba-48a7-8bf1-7a3e3a885eb5", true},
		{"canonical uppercase", "ABCDEF01-2345-6789-ABCD-EF0123456789", true},
		{"mixed case", "aBcDeF01-2345-6789-abCD-eF0123456789", true},
		{"resource name", "bmw-staging", false},
		{"empty", "", false},
		{"too short", "f8b6200c-3bba-48a7-8bf1", false},
		{"too long", "f8b6200c-3bba-48a7-8bf1-7a3e3a885eb5-extra", false},
		{"missing hyphen 1", "f8b6200c03bba-48a7-8bf1-7a3e3a885eb5", false},
		{"missing hyphen 2", "f8b6200c-3bba048a7-8bf1-7a3e3a885eb5", false},
		{"non-hex character", "f8b6200c-3bba-48a7-8bf1-7a3e3a885ebZ", false},
		{"36 chars but wrong hyphens", "f8b6200c-3bba048a7-8bf1-7a3e3a885ebX", false},
		{"spaces", "f8b6200c 3bba 48a7 8bf1 7a3e3a885eb5 ", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := validateUUID(c.in); got != c.want {
				t.Errorf("validateUUID(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestValidateDomainName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"simple", "example.com", true},
		{"subdomain", "api.example.com", true},
		{"hyphens ok in middle", "my-app.example.com", true},
		{"single label", "localhost", true},
		{"numeric label", "1.example.com", true},
		{"label all digits", "example.123.com", true},
		{"empty", "", false},
		{"leading hyphen", "-bad.com", false},
		{"trailing hyphen", "bad-.com", false},
		{"label too long (64)", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.com", false},
		{"total too long (254)", "a." + string(make([]byte, 252)), false},
		{"consecutive dots", "a..b.com", false},
		{"trailing dot is ok", "example.com.", true},
		{"underscore in label", "my_app.com", false},
		{"space", "my app.com", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := validateDomainName(c.in); got != c.want {
				t.Errorf("validateDomainName(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestValidateARN(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"canonical s3 bucket", "arn:aws:s3:::my-bucket", true},
		{"canonical iam", "arn:aws:iam::123456789012:role/MyRole", true},
		{"canonical lambda", "arn:aws:lambda:us-east-1:123456789012:function:myfn", true},
		{"aws-cn partition", "arn:aws-cn:s3:::my-bucket", true},
		{"aws-us-gov partition", "arn:aws-us-gov:s3:::my-bucket", true},
		{"empty", "", false},
		{"missing prefix", "s3:::my-bucket", false},
		{"only five segments", "arn:aws:s3::my-bucket", false},
		{"not starting with arn", "aws:s3:::my-bucket", false},
		{"empty partition", "arn::s3:::my-bucket", false},
		{"empty service", "arn:aws::::my-bucket", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := validateARN(c.in); got != c.want {
				t.Errorf("validateARN(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestValidateProviderID_Dispatch(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		format ProviderIDFormat
		want   bool
	}{
		{"uuid ok", "f8b6200c-3bba-48a7-8bf1-7a3e3a885eb5", IDFormatUUID, true},
		{"uuid bad", "bmw-staging", IDFormatUUID, false},
		{"domain ok", "example.com", IDFormatDomainName, true},
		{"domain bad", "not a domain", IDFormatDomainName, false},
		{"arn ok", "arn:aws:s3:::bucket", IDFormatARN, true},
		{"arn bad", "nope", IDFormatARN, false},
		{"freeform accepts any non-empty", "whatever", IDFormatFreeform, true},
		{"freeform rejects empty", "", IDFormatFreeform, false},
		{"unknown accepts anything", "literally-anything", IDFormatUnknown, true},
		{"unknown accepts empty", "", IDFormatUnknown, true},
		{"unrecognized format accepts anything (forward compat)", "anything", ProviderIDFormat(99), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ValidateProviderID(c.in, c.format); got != c.want {
				t.Errorf("ValidateProviderID(%q, %v) = %v, want %v", c.in, c.format, got, c.want)
			}
		})
	}
}
