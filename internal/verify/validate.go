package verify

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws/arn"
	"github.com/hashicorp/go-cty/cty"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/structure"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/validation"
)

var accountIDRegexp = regexp.MustCompile(`^(aws|aws-managed|third-party|\d{12})$`)
var partitionRegexp = regexp.MustCompile(`^aws(-[a-z]+)*$`)
var regionRegexp = regexp.MustCompile(`^[a-z]{2}(-[a-z]+)+-\d$`)

func Valid4ByteASN(v interface{}, k string) (ws []string, errors []error) {
	value := v.(string)

	asn, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		errors = append(errors, fmt.Errorf("%q (%q) must be a 64-bit integer", k, v))
		return
	}

	if asn < 0 || asn > 4294967295 {
		errors = append(errors, fmt.Errorf("%q (%q) must be in the range 0 to 4294967295", k, v))
	}
	return
}

// ValidARN validates that a string value matches a generic ARN format
var ValidARN = ValidARNCheck()

type ARNCheckFunc func(any, string, arn.ARN) ([]string, []error)

// ValidARNCheck validates that a string value matches an ARN format with additional validation on the parsed ARN value
// It must:
// * Be parseable as an ARN
// * Have a valid partition
// * Have a valid region
// * Have either an empty or valid account ID
// * Have a non-empty resource part
// * Pass the supplied checks
func ValidARNCheck(f ...ARNCheckFunc) schema.SchemaValidateFunc {
	return func(v any, k string) (ws []string, errors []error) {
		value, ok := v.(string)
		if !ok {
			errors = append(errors, fmt.Errorf("expected type of %s to be string", k))
			return ws, errors
		}

		if value == "" {
			return ws, errors
		}

		parsedARN, err := arn.Parse(value)

		if err != nil {
			errors = append(errors, fmt.Errorf("%q (%s) is an invalid ARN: %s", k, value, err))
			return ws, errors
		}

		if parsedARN.Partition == "" {
			errors = append(errors, fmt.Errorf("%q (%s) is an invalid ARN: missing partition value", k, value))
		} else if !partitionRegexp.MatchString(parsedARN.Partition) {
			errors = append(errors, fmt.Errorf("%q (%s) is an invalid ARN: invalid partition value (expecting to match regular expression: %s)", k, value, partitionRegexp))
		}

		if parsedARN.Region != "" && !regionRegexp.MatchString(parsedARN.Region) {
			errors = append(errors, fmt.Errorf("%q (%s) is an invalid ARN: invalid region value (expecting to match regular expression: %s)", k, value, regionRegexp))
		}

		if parsedARN.AccountID != "" && !accountIDRegexp.MatchString(parsedARN.AccountID) {
			errors = append(errors, fmt.Errorf("%q (%s) is an invalid ARN: invalid account ID value (expecting to match regular expression: %s)", k, value, accountIDRegexp))
		}

		if parsedARN.Resource == "" {
			errors = append(errors, fmt.Errorf("%q (%s) is an invalid ARN: missing resource value", k, value))
		}

		for _, f := range f {
			w, e := f(v, k, parsedARN)
			ws = append(ws, w...)
			errors = append(errors, e...)
		}

		return ws, errors
	}
}

func ValidAccountID(v interface{}, k string) (ws []string, errors []error) {
	value := v.(string)

	// http://docs.aws.amazon.com/lambda/latest/dg/API_AddPermission.html
	pattern := `^\d{12}$`
	if !regexp.MustCompile(pattern).MatchString(value) {
		errors = append(errors, fmt.Errorf(
			"%q doesn't look like AWS Account ID (exactly 12 digits): %q",
			k, value))
	}

	return
}

// ValidateCIDRBlock validates that the specified CIDR block is valid:
// - The CIDR block parses to an IP address and network
// - The CIDR block is the CIDR block for the network
func ValidateCIDRBlock(cidr string) error {
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("%q is not a valid CIDR block: %w", cidr, err)
	}

	if !CIDRBlocksEqual(cidr, ipnet.String()) {
		return fmt.Errorf("%q is not a valid CIDR block; did you mean %q?", cidr, ipnet)
	}

	return nil
}

// ValidCIDRNetworkAddress ensures that the string value is a valid CIDR that
// represents a network address - it adds an error otherwise
func ValidCIDRNetworkAddress(v interface{}, k string) (ws []string, errors []error) {
	if err := ValidateCIDRBlock(v.(string)); err != nil {
		errors = append(errors, err)
		return
	}

	return
}

func ValidIAMPolicyJSON(v interface{}, k string) (ws []string, errors []error) {
	// IAM Policy documents need to be valid JSON, and pass legacy parsing
	value := v.(string)
	if len(value) < 1 {
		errors = append(errors, fmt.Errorf("%q contains an invalid JSON policy", k))
		return
	}
	if value[:1] != "{" {
		errors = append(errors, fmt.Errorf("%q contains an invalid JSON policy", k))
		return
	}
	if _, err := structure.NormalizeJsonString(v); err != nil {
		errors = append(errors, fmt.Errorf("%q contains an invalid JSON: %s", k, err))
	}
	return
}

// ValidateIPv4CIDRBlock validates that the specified CIDR block is valid:
// - The CIDR block parses to an IP address and network
// - The IP address is an IPv4 address
// - The CIDR block is the CIDR block for the network
func ValidateIPv4CIDRBlock(cidr string) error {
	ip, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("%q is not a valid CIDR block: %w", cidr, err)
	}

	ipv4 := ip.To4()
	if ipv4 == nil {
		return fmt.Errorf("%q is not a valid IPv4 CIDR block", cidr)
	}

	if !CIDRBlocksEqual(cidr, ipnet.String()) {
		return fmt.Errorf("%q is not a valid IPv4 CIDR block; did you mean %q?", cidr, ipnet)
	}

	return nil
}

// ValidateIPv6CIDRBlock validates that the specified CIDR block is valid:
// - The CIDR block parses to an IP address and network
// - The IP address is an IPv6 address
// - The CIDR block is the CIDR block for the network
func ValidateIPv6CIDRBlock(cidr string) error {
	ip, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return fmt.Errorf("%q is not a valid CIDR block: %w", cidr, err)
	}

	ipv4 := ip.To4()
	if ipv4 != nil {
		return fmt.Errorf("%q is not a valid IPv6 CIDR block", cidr)
	}

	if !CIDRBlocksEqual(cidr, ipnet.String()) {
		return fmt.Errorf("%q is not a valid IPv6 CIDR block; did you mean %q?", cidr, ipnet)
	}

	return nil
}

// ValidIPv4CIDRNetworkAddress ensures that the string value is a valid IPv4 CIDR that
// represents a network address - it adds an error otherwise
func ValidIPv4CIDRNetworkAddress(v interface{}, k string) (ws []string, errors []error) {
	if err := ValidateIPv4CIDRBlock(v.(string)); err != nil {
		errors = append(errors, err)
		return
	}

	return
}

// ValidIPv6CIDRNetworkAddress ensures that the string value is a valid IPv6 CIDR that
// represents a network address - it adds an error otherwise
func ValidIPv6CIDRNetworkAddress(v interface{}, k string) (ws []string, errors []error) {
	if err := ValidateIPv6CIDRBlock(v.(string)); err != nil {
		errors = append(errors, err)
		return
	}

	return
}

// IsIPv4CIDRBlockOrIPv6CIDRBlock returns a SchemaValidateFunc that test if the provided value:
// - Is a valid IPv4 CIDR block and passes the specified validation, or
// - Is a valid IPv6 CIDR block and passes the specified validation
func IsIPv4CIDRBlockOrIPv6CIDRBlock(ipv4Validator, ipv6Validator schema.SchemaValidateFunc) schema.SchemaValidateFunc {
	return validation.Any(
		validation.All(ValidIPv4CIDRNetworkAddress, ipv4Validator),
		validation.All(ValidIPv6CIDRNetworkAddress, ipv6Validator),
	)
}

// KMS Key IDs (a subset of KMS Key Identifiers) can be be key ID, key ARN, alias name, or alias ARN.
// There's no guarantee about the format of a Key ID other than a string between 1 and 2048 characters
// (per KMS API documentation and internal AWS conversations).
// ref: https://docs.aws.amazon.com/kms/latest/developerguide/concepts.html#key-id
// ref: https://docs.aws.amazon.com/kms/latest/APIReference/API_Encrypt.html#KMS-Encrypt-request-KeyId
func ValidKMSKeyID(v interface{}, k string) (ws []string, errors []error) {
	value := v.(string)
	if len(value) < 1 {
		errors = append(errors, fmt.Errorf("%q cannot be shorter than 1 character", k))
	} else if len(value) > 2048 {
		errors = append(errors, fmt.Errorf("%q cannot be longer than 2048 characters", k))
	}
	return
}

func ValidLaunchTemplateID(v interface{}, k string) (ws []string, errors []error) {
	value := v.(string)
	if len(value) < 1 {
		errors = append(errors, fmt.Errorf("%q cannot be shorter than 1 character", k))
	} else if len(value) > 255 {
		errors = append(errors, fmt.Errorf("%q cannot be longer than 255 characters", k))
	} else if !regexp.MustCompile(`^lt\-[a-z0-9]+$`).MatchString(value) {
		errors = append(errors, fmt.Errorf(
			"%q must begin with 'lt-' and be comprised of only alphanumeric characters: %v", k, value))
	}
	return
}

func ValidLaunchTemplateName(v interface{}, k string) (ws []string, errors []error) {
	value := v.(string)
	if len(value) < 3 {
		errors = append(errors, fmt.Errorf("%q cannot be less than 3 characters", k))
	} else if strings.HasSuffix(k, "prefix") && len(value) > 99 {
		errors = append(errors, fmt.Errorf("%q cannot be longer than 99 characters, name is limited to 125", k))
	} else if !strings.HasSuffix(k, "prefix") && len(value) > 125 {
		errors = append(errors, fmt.Errorf("%q cannot be longer than 125 characters", k))
	} else if !regexp.MustCompile(`^[0-9a-zA-Z()./_\-]+$`).MatchString(value) {
		errors = append(errors, fmt.Errorf("%q can only alphanumeric characters and ()./_- symbols", k))
	}
	return
}

// validateMulticastIPAddress validates that the specified string is a multicast IP address.
func validateMulticastIPAddress(s string) error {
	ip := net.ParseIP(s)
	if ip == nil {
		return fmt.Errorf("%q is not a valid IP address", s)
	}

	if !ip.IsMulticast() {
		return fmt.Errorf("%q is not a valid multicast address", s)
	}

	return nil
}

func ValidMulticastIPAddress(v interface{}, k string) (ws []string, errors []error) {
	if err := validateMulticastIPAddress(v.(string)); err != nil {
		errors = append(errors, err)
		return
	}

	return
}

func ValidateOnceADayWindowFormat(value string) error {
	// valid time format is "hh24:mi"
	validTimeFormat := "([0-1][0-9]|2[0-3]):([0-5][0-9])"
	validTimeFormatConsolidated := "^(" + validTimeFormat + "-" + validTimeFormat + "|)$"

	if !regexp.MustCompile(validTimeFormatConsolidated).MatchString(value) {
		return fmt.Errorf("(%s) must satisfy the format of \"hh24:mi-hh24:mi\"", value)
	}

	return nil
}

func ValidOnceADayWindowFormat(v interface{}, k string) (ws []string, errors []error) {
	value := v.(string)

	if err := ValidateOnceADayWindowFormat(value); err != nil {
		errors = append(errors, err)
		return
	}

	return
}

func ValidateOnceAWeekWindowFormat(value string) error {
	// valid time format is "ddd:hh24:mi"
	validTimeFormat := "(sun|mon|tue|wed|thu|fri|sat):([0-1][0-9]|2[0-3]):([0-5][0-9])"
	validTimeFormatConsolidated := "^(" + validTimeFormat + "-" + validTimeFormat + "|)$"

	val := strings.ToLower(value)
	if !regexp.MustCompile(validTimeFormatConsolidated).MatchString(val) {
		return fmt.Errorf("(%s) must satisfy the format of \"ddd:hh24:mi-ddd:hh24:mi\"", val)
	}

	return nil
}

func ValidOnceAWeekWindowFormat(v interface{}, k string) (ws []string, errors []error) {
	value := v.(string)

	if err := ValidateOnceAWeekWindowFormat(value); err != nil {
		errors = append(errors, err)
		return
	}

	return
}

func ValidRegionName(v interface{}, k string) (ws []string, errors []error) {
	value := v.(string)

	if value == "" {
		return ws, errors
	}
	if !regionRegexp.MatchString(value) {
		errors = append(errors, fmt.Errorf(
			"%q region name is malformed(%q): %q",
			k, regionRegexp, value))
	}

	return
}

func ValidStringIsJSONOrYAML(v interface{}, k string) (ws []string, errors []error) {
	if looksLikeJSONString(v) {
		if _, err := structure.NormalizeJsonString(v); err != nil {
			errors = append(errors, fmt.Errorf("%q contains an invalid JSON: %s", k, err))
		}
	} else {
		if _, err := checkYAMLString(v); err != nil {
			errors = append(errors, fmt.Errorf("%q contains an invalid YAML: %s", k, err))
		}
	}
	return
}

// ValidTypeStringNullableFloat provides custom error messaging for TypeString floats
// Some arguments require a floating point value or an unspecified, empty field.
func ValidTypeStringNullableFloat(v interface{}, k string) (ws []string, es []error) {
	value, ok := v.(string)
	if !ok {
		es = append(es, fmt.Errorf("expected type of %s to be string", k))
		return
	}

	if value == "" {
		return
	}

	if _, err := strconv.ParseFloat(value, 64); err != nil {
		es = append(es, fmt.Errorf("%s: cannot parse '%s' as float: %s", k, value, err))
	}

	return
}

func ValidateUTCTimestamp(value string) error {
	_, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return fmt.Errorf("must be in RFC3339 time format %q. Example: %s", time.RFC3339, err)
	}

	return nil
}

// ValidUTCTimestamp validates a string in UTC Format required by APIs including:
// https://docs.aws.amazon.com/iot/latest/apireference/API_CloudwatchMetricAction.html
// https://docs.aws.amazon.com/AmazonRDS/latest/APIReference/API_RestoreDBInstanceToPointInTime.html
func ValidUTCTimestamp(v interface{}, k string) (ws []string, errors []error) {
	value := v.(string)
	if err := ValidateUTCTimestamp(value); err != nil {
		errors = append(errors, err)
		return
	}

	return
}

var ValidStringDateOrPositiveInt = validation.Any(
	validation.IsRFC3339Time,
	validation.StringMatch(regexp.MustCompile(`^\d+$`), "must be a positive integer value"),
)

func ValidDuration(v interface{}, k string) (ws []string, errors []error) {
	value := v.(string)
	duration, err := time.ParseDuration(value)
	if err != nil {
		errors = append(errors, fmt.Errorf("%q cannot be parsed as a duration: %s", k, err))
	}
	if duration < 0 {
		errors = append(errors, fmt.Errorf("%q must be greater than zero", k))
	}
	return
}

// FloatGreaterThan returns a SchemaValidateFunc which tests if the provided value
// is of type float and is greater than threshold.
func FloatGreaterThan(threshold float64) schema.SchemaValidateFunc {
	return func(i interface{}, k string) (s []string, es []error) {
		v, ok := i.(float64)
		if !ok {
			es = append(es, fmt.Errorf("expected type of %s to be float", k))
			return
		}

		if v <= threshold {
			es = append(es, fmt.Errorf("expected %s to be greater than (%f), got %f", k, threshold, v))
			return
		}

		return
	}
}

// https://github.com/hashicorp/terraform-plugin-sdk/issues/780.
func ValidAllDiag(validators ...schema.SchemaValidateDiagFunc) schema.SchemaValidateDiagFunc {
	return func(i any, path cty.Path) diag.Diagnostics {
		var results diag.Diagnostics
		for _, validator := range validators {
			results = append(results, validator(i, path)...)
		}
		return results
	}
}

// https://github.com/hashicorp/terraform-plugin-sdk/issues/780.
func ValidAnyDiag(validators ...schema.SchemaValidateDiagFunc) schema.SchemaValidateDiagFunc {
	return func(i any, path cty.Path) diag.Diagnostics {
		var results diag.Diagnostics
		for _, validator := range validators {
			diags := validator(i, path)
			if len(diags) == 0 {
				return diag.Diagnostics{}
			}
			results = append(results, diags...)
		}
		return results
	}
}
