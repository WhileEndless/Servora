package agent

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/WhileEndless/Servora/internal/model"
)

func packageID(manager, name, architecture string) string {
	raw := manager + "\x00" + name + "\x00" + architecture
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodePackageID(id string) (string, string, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil {
		return "", "", "", errors.New("invalid package id")
	}
	parts := strings.Split(string(raw), "\x00")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", "", errors.New("invalid package id")
	}
	return parts[0], parts[1], parts[2], nil
}

func scanPackages(ctx context.Context, refreshMetadata bool) (model.PackageScan, error) {
	host, _ := os.Hostname()
	result := model.PackageScan{Hostname: host, InventoryScannedAt: time.Now()}
	switch {
	case commandExists("dpkg-query"):
		result.Manager = "apt"
		result.InventoryAvailable = true
		result.UpdateCheckAvailable = commandExists("apt-get")
		if refreshMetadata && result.UpdateCheckAvailable {
			if out, err := packageCommand(ctx, "apt-get", "-o", "DPkg::Lock::Timeout=30", "update"); err != nil {
				result.Error = "APT metadata refresh failed: " + conciseCommandError(out)
			} else {
				result.MetadataRefreshedAt = time.Now()
			}
		}
		items, err := scanDPKG(ctx)
		if err != nil {
			return result, err
		}
		if result.UpdateCheckAvailable {
			updates, err := aptUpdates(ctx)
			if err != nil && result.Error == "" {
				result.Error = "APT update check failed: " + err.Error()
			} else if err == nil {
				for index := range items {
					if candidate := updates[items[index].Name+"\x00"+items[index].Architecture]; candidate != "" {
						items[index].CandidateVersion = candidate
						items[index].UpdateState = "update_available"
					} else {
						items[index].UpdateState = "current"
					}
				}
			}
		}
		result.Items = items
	case commandExists("rpm"):
		result.Manager = "dnf"
		result.InventoryAvailable = true
		result.UpdateCheckAvailable = commandExists("dnf")
		if refreshMetadata && result.UpdateCheckAvailable {
			if out, err := packageCommand(ctx, "dnf", "-q", "makecache", "--refresh"); err != nil {
				result.Error = "DNF metadata refresh failed: " + conciseCommandError(out)
			} else {
				result.MetadataRefreshedAt = time.Now()
			}
		}
		items, err := scanRPM(ctx)
		if err != nil {
			return result, err
		}
		if result.UpdateCheckAvailable {
			updates, err := dnfUpdates(ctx)
			if err != nil && result.Error == "" {
				result.Error = "DNF update check failed: " + err.Error()
			} else if err == nil {
				for index := range items {
					if candidate := updates[items[index].Name+"\x00"+items[index].Architecture]; candidate != "" {
						items[index].CandidateVersion = candidate
						items[index].UpdateState = "update_available"
					} else {
						items[index].UpdateState = "current"
					}
				}
			}
		}
		result.Items = items
	default:
		return result, errors.New("no supported system package database was detected")
	}
	return result, nil
}

func scanDPKG(ctx context.Context) ([]model.Package, error) {
	format := `${binary:Package}\t${Version}\t${Architecture}\t${db:Status-Abbrev}\t${Installed-Size}\t${source:Package}\t${binary:Summary}\n`
	out, err := packageCommand(ctx, "dpkg-query", "-W", "-f="+format)
	if err != nil {
		return nil, fmt.Errorf("dpkg inventory failed: %s", conciseCommandError(out))
	}
	var result []model.Package
	for _, line := range strings.Split(out, "\n") {
		fields := strings.SplitN(line, "\t", 7)
		if len(fields) < 5 || !strings.HasPrefix(fields[3], "ii") {
			continue
		}
		name := fields[0]
		if base, _, found := strings.Cut(name, ":"); found {
			name = base
		}
		sizeKiB, _ := strconv.ParseUint(fields[4], 10, 64)
		item := model.Package{
			ID: packageID("apt", name, fields[2]), Manager: "apt", Name: name,
			Architecture: fields[2], InstalledVersion: fields[1], UpdateState: "unknown",
			InstalledSizeBytes: sizeKiB * 1024,
		}
		if len(fields) > 5 {
			item.Source = fields[5]
		}
		if len(fields) > 6 {
			item.Summary = fields[6]
		}
		result = append(result, item)
	}
	return result, nil
}

func aptUpdates(ctx context.Context) (map[string]string, error) {
	out, err := packageCommand(ctx, "apt-get", "-s", "-o", "Debug::NoLocking=1", "upgrade")
	if err != nil {
		return nil, errors.New(conciseCommandError(out))
	}
	return parseAPTUpdates(out, nativeArchitecture(ctx)), nil
}

func parseAPTUpdates(out, nativeArchitecture string) map[string]string {
	result := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "Inst ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		name, architecture := fields[1], ""
		if base, arch, found := strings.Cut(name, ":"); found {
			name, architecture = base, arch
		}
		for index := len(fields) - 1; index >= 2 && architecture == ""; index-- {
			field := fields[index]
			if strings.HasPrefix(field, "[") && (strings.HasSuffix(field, "]") || strings.HasSuffix(field, "])")) {
				candidateArch := strings.Trim(field, "[]()")
				if strings.ContainsAny(candidateArch, "abcdefghijklmnopqrstuvwxyz") &&
					!strings.ContainsAny(candidateArch, ".:+~") {
					architecture = candidateArch
				}
			}
		}
		for _, field := range fields[3:] {
			if strings.HasPrefix(field, "(") {
				candidate := strings.TrimPrefix(field, "(")
				result[name+"\x00"+architecture] = candidate
				if architecture == "" {
					result[name+"\x00"+nativeArchitecture] = candidate
				}
				break
			}
		}
	}
	return result
}

func nativeArchitecture(ctx context.Context) string {
	out, _ := packageCommand(ctx, "dpkg", "--print-architecture")
	return strings.TrimSpace(out)
}

func scanRPM(ctx context.Context) ([]model.Package, error) {
	format := "%{NAME}\\t%|EPOCH?{%{EPOCH}:}:{}|%{VERSION}-%{RELEASE}\\t%{ARCH}\\t%{SIZE}\\t%{VENDOR}\\t%{SUMMARY}\\n"
	out, err := packageCommand(ctx, "rpm", "-qa", "--qf", format)
	if err != nil {
		return nil, fmt.Errorf("rpm inventory failed: %s", conciseCommandError(out))
	}
	var result []model.Package
	for _, line := range strings.Split(out, "\n") {
		fields := strings.SplitN(line, "\t", 6)
		if len(fields) < 4 || fields[0] == "" {
			continue
		}
		size, _ := strconv.ParseUint(fields[3], 10, 64)
		item := model.Package{ID: packageID("dnf", fields[0], fields[2]), Manager: "dnf",
			Name: fields[0], InstalledVersion: fields[1], Architecture: fields[2],
			InstalledSizeBytes: size, UpdateState: "unknown"}
		if len(fields) > 4 {
			item.Source = fields[4]
		}
		if len(fields) > 5 {
			item.Summary = fields[5]
		}
		result = append(result, item)
	}
	return result, nil
}

func dnfUpdates(ctx context.Context) (map[string]string, error) {
	format := "%{name}\\t%|epoch?{%{epoch}:}:{}|%{version}-%{release}\\t%{arch}\\n"
	out, err := packageCommand(ctx, "dnf", "-q", "repoquery", "--upgrades", "--latest-limit=1", "--qf", format)
	if err != nil {
		return nil, errors.New(conciseCommandError(out))
	}
	result := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) == 3 {
			result[fields[0]+"\x00"+fields[2]] = fields[1]
		}
	}
	return result, nil
}

func packageFiles(ctx context.Context, id string) ([]string, error) {
	manager, name, architecture, err := decodePackageID(id)
	if err != nil {
		return nil, err
	}
	var out string
	switch manager {
	case "apt":
		out, err = packageCommand(ctx, "dpkg-query", "-L", name+":"+architecture)
	case "dnf":
		out, err = packageCommand(ctx, "rpm", "-ql", name+"."+architecture)
	default:
		return nil, errors.New("unsupported package manager")
	}
	if err != nil {
		return nil, errors.New(conciseCommandError(out))
	}
	var files []string
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

func packageCommand(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C", "DEBIAN_FRONTEND=noninteractive")
	raw, err := cmd.CombinedOutput()
	if len(raw) > 32<<20 {
		return string(raw[:32<<20]), errors.New("package command output exceeded 32 MiB")
	}
	return string(raw), err
}
