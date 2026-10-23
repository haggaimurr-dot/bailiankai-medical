// 开发者工具使用本机地址。目前服务端仅监听 loopback，不能直接真机访问。
// 真机验证需先提供受控测试服务，再设置对应地址和微信 request 合法域名。
// 正式环境必须使用已配置的 HTTPS 域名，并开启 URL 校验。
export const API_BASE_URL = 'http://127.0.0.1:8080/api/v1';
// 虚构数据验收可改为 demo；微信凭据仅保存在服务端。
export const PATIENT_LOGIN_MODE: 'wechat' | 'demo' = 'wechat';
