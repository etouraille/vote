import { HttpClient } from '@angular/common/http';
import { Service, inject } from '@angular/core';
import { Observable } from 'rxjs';
import { API_BASE_URL } from '../api-base-url';
import { AdminUser, Permissions } from '../model/admin.model';

@Service()
export class AdminService {
  private readonly http = inject(HttpClient);

  listUsers(): Observable<AdminUser[]> {
    return this.http.get<AdminUser[]>(`${API_BASE_URL}/api/admin/users`);
  }

  updatePermissions(userId: string, root: boolean, permissions: Permissions): Observable<unknown> {
    return this.http.put(`${API_BASE_URL}/api/admin/users/${userId}/permissions`, { root, ...permissions });
  }

  deleteUser(userId: string): Observable<unknown> {
    return this.http.delete(`${API_BASE_URL}/api/admin/users/${userId}`);
  }
}
