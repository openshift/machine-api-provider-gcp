package util

import "testing"

func TestCPUArchitecture(t *testing.T) {
	type args struct {
		machineType string
	}
	tests := []struct {
		name string
		args args
		want NormalizedArch
	}{
		{
			name: "should return arm64 for t2a-* machine types",
			args: args{
				machineType: "t2a-standard-8",
			},
			want: ArchitectureArm64,
		},
		{
			name: "should return amd64 for unknown machine types as fallback (n2-standard-8)",
			args: args{
				machineType: "n2-standard-8",
			},
			want: ArchitectureAmd64,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CPUArchitecture(tt.args.machineType); got != tt.want {
				t.Errorf("CPUArchitecture() = %v, want %v", got, tt.want)
			}
		})
	}
}
