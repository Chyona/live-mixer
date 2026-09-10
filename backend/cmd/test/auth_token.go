package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"live-mixer/internal/config"
	jwtpkg "live-mixer/internal/pkg/jwt"
)

type namedJWTSecret struct {
	Source string
	Secret string
}

// resolveHTTPToken 优先本地签发 JWT（与 webserver 共用密钥），避免密码登录。
// 密钥候选顺序：
//  1. -token
//  2. -jwt-secret
//  3. 配置文件/内嵌 jwt.secret（忽略 APP_JWT_SECRET，对齐本地 webserver.exe）
//  4. APP_JWT_SECRET（含 docker/.env，对齐 compose）
// 对每个签发结果请求一次需鉴权接口，采用首个非 401 的 token。
func resolveHTTPToken(
	ctx context.Context,
	client *http.Client,
	base, token, username, password, configPath, jwtSecret string,
	userID uint,
	forceLogin bool,
) (string, error) {
	if t := strings.TrimSpace(token); t != "" {
		return t, nil
	}
	uid := userID
	if uid == 0 {
		uid = 1
	}
	user := firstNonEmpty(strings.TrimSpace(username), os.Getenv("LIVE_MIXER_USER"), "admin")

	if !forceLogin {
		for _, cand := range jwtSecretCandidates(configPath, jwtSecret) {
			t, err := mintTokenWithSecret(cand.Secret, uid, user, 86400)
			if err != nil {
				fmt.Fprintf(os.Stderr, "警告: 签发失败 source=%s: %v\n", cand.Source, err)
				continue
			}
			if !httpBearerAccepted(ctx, client, base, t) {
				fmt.Fprintf(os.Stderr, "警告: token 不被 webserver 接受 source=%s（密钥可能不一致）\n", cand.Source)
				continue
			}
			fmt.Printf("已用配置 JWT 签发 token（uid=%d user=%s source=%s）\n", uid, user, cand.Source)
			return t, nil
		}
		fmt.Fprintln(os.Stderr, "警告: 所有本地 JWT 密钥均未被接受，回退密码登录")
	}

	pass := firstNonEmpty(strings.TrimSpace(password), os.Getenv("LIVE_MIXER_PASSWORD"), "admin")
	t, err := apiLogin(ctx, client, base, user, pass)
	if err != nil {
		return "", err
	}
	fmt.Printf("已登录 %s @ %s\n", user, base)
	return t, nil
}

func jwtSecretCandidates(configPath, explicit string) []namedJWTSecret {
	var out []namedJWTSecret
	seen := map[string]struct{}{}
	add := func(source, secret string) {
		secret = strings.TrimSpace(secret)
		if secret == "" {
			return
		}
		if _, ok := seen[secret]; ok {
			return
		}
		seen[secret] = struct{}{}
		out = append(out, namedJWTSecret{Source: source, Secret: secret})
	}

	add("-jwt-secret", explicit)

	// 与「直接跑 webserver.exe、未注入 APP_JWT_SECRET」对齐：先读文件/内嵌，暂时屏蔽环境变量。
	envSecret, envHad := os.LookupEnv("APP_JWT_SECRET")
	_ = os.Unsetenv("APP_JWT_SECRET")
	if cfg, err := config.Load(configPath); err == nil {
		add("config.yaml/embed", cfg.JWT.Secret)
	}
	if envHad {
		_ = os.Setenv("APP_JWT_SECRET", envSecret)
		add("APP_JWT_SECRET", envSecret)
	}

	return out
}

func mintTokenWithSecret(secret string, userID uint, username string, expiresIn int) (string, error) {
	if expiresIn <= 0 {
		expiresIn = 86400
	}
	if userID == 0 {
		userID = 1
	}
	if strings.TrimSpace(username) == "" {
		username = "admin"
	}
	return jwtpkg.GenerateToken(secret, expiresIn, jwtpkg.UserClaims{
		UserID:   userID,
		Username: username,
		Nickname: "测试",
		Roles:    []string{"ADMIN"},
	})
}

// httpBearerAccepted 用需登录接口探测 token 是否被当前 webserver 接受。
func httpBearerAccepted(ctx context.Context, client *http.Client, base, token string) bool {
	url := apiURL(base, "/v1/live-materials") + "?page=1&page_size=1"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode != http.StatusUnauthorized
}
