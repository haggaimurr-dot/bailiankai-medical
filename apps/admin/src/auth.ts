import { reactive } from 'vue'

export type Employee = { id: string; username: string; displayName: string; role: 'manager' | 'clinician' | 'doctor' | 'nurse' | 'customer_service' | 'operations'; clinicIds: string[]; staffId: string | null; active: boolean; version: number; capabilities?: string[] }
const key = 'bailiankai.employee-session'
function stored(): { token: string; employee: Employee | null } {
  try {
    const value = JSON.parse(sessionStorage.getItem(key) || '{}')
    return { token: typeof value.token === 'string' ? value.token : '', employee: value.employee || null }
  } catch { return { token: '', employee: null } }
}
export const auth = reactive(stored())
export function setAuth(token: string, employee: Employee) {
  auth.token = token; auth.employee = employee
  sessionStorage.setItem(key, JSON.stringify({ token, employee }))
}
export function clearAuth() {
  auth.token = ''; auth.employee = null; sessionStorage.removeItem(key)
}

export function canAccess(key: string) {
  if (key.startsWith('/assessments/')) key = '/assessments'
  return auth.employee?.capabilities?.includes(key) === true
}
export function employeeHome() { return ['/appointments', '/overview', '/followups'].find(canAccess) || auth.employee?.capabilities?.find(key => key.startsWith('/') && key !== '/account') || '/account' }
export const roleNames: Record<Employee['role'], string> = { manager: '主管', clinician: '医护（兼容旧账号）', doctor: '医生', nurse: '护理', customer_service: '客服', operations: '运营' }
