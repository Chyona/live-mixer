package utils

// ComparePassword 校验明文密码是否与哈希值匹配。
func ComparePassword(hashed, password string) bool {
	sum, err := HashPassword(password)
	if err != nil {
		return false
	}
	return sum == hashed
}

// ParseRoles 将逗号分隔的角色字符串解析为数组，空字符串返回 nil。
func ParseRoles(roles string) []string {
	if roles == "" {
		return nil
	}
	parts := make([]string, 0)
	start := 0
	for i := 0; i <= len(roles); i++ {
		if i == len(roles) || roles[i] == ',' {
			part := roles[start:i]
			start = i + 1
			if part == "" {
				continue
			}
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 {
		return nil
	}
	return parts
}
