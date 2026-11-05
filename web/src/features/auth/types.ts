// 认证域数据形状（H-01/H-02/H-04）：网关 protojson 使用 UseProtoNames=true → snake_case，
// int64 字段（created_at 等）序列化为字符串。事实源 excel_web_api/internal/gateway/*.go。
export interface AuthUser {
  id: string
  username: string
  display_name?: string
  avatar_uri?: string
  enterprise_id?: string
  created_at?: string
}

export interface AuthEnterprise {
  id: string
  name: string
  owner_id?: string
  member_count?: number
  created_at?: string
}

/** H-02/H-01 成功 data：token + user（登录后由网关设置 session Cookie）。 */
export interface AuthCredential {
  token: string
  user: AuthUser
}

/** H-04 GET /auth/me data：身份事实源。 */
export interface AuthMeData {
  user: AuthUser
  enterprise?: AuthEnterprise
  my_role?: string
}
