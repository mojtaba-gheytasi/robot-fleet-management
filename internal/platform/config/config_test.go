package config

import "testing"

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		want    Config
		wantErr bool
	}{
		{
			name: "defaults the HTTP address to loopback",
			env:  map[string]string{"DATABASE_URL": "postgres://db"},
			want: Config{DatabaseURL: "postgres://db", HTTPServerAddress: "127.0.0.1:8080"},
		},
		{
			name: "uses the configured HTTP address",
			env:  map[string]string{"DATABASE_URL": "postgres://db", "HTTP_SERVER_ADDRESS": ":9090"},
			want: Config{DatabaseURL: "postgres://db", HTTPServerAddress: ":9090"},
		},
		{
			name:    "requires DATABASE_URL",
			env:     map[string]string{"HTTP_SERVER_ADDRESS": ":9090"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Load(func(key string) string { return tt.env[key] })

			if tt.wantErr {
				if err == nil {
					t.Fatalf("Load() error = nil, want an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("Load() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
