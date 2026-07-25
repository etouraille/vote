import { HttpErrorResponse } from '@angular/common/http';
import { Component, OnInit, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { RouterLink } from '@angular/router';
import { AdminUser, PERMISSION_KEYS, PERMISSION_LABELS } from '../../model/admin.model';
import { AdminService } from '../../service/admin';

@Component({
  selector: 'backoffice-page',
  imports: [FormsModule, RouterLink],
  templateUrl: './backoffice.html',
})
export class BackofficePage implements OnInit {
  private readonly adminService = inject(AdminService);

  readonly permissionKeys = PERMISSION_KEYS;
  readonly permissionLabels = PERMISSION_LABELS;

  readonly users = signal<AdminUser[]>([]);
  readonly loading = signal(true);
  readonly error = signal<string | null>(null);
  readonly savingId = signal<string | null>(null);
  readonly savedId = signal<string | null>(null);
  readonly deletingId = signal<string | null>(null);

  ngOnInit(): void {
    this.load();
  }

  load(): void {
    this.loading.set(true);
    this.error.set(null);
    this.adminService.listUsers().subscribe({
      next: (users) => {
        this.loading.set(false);
        this.users.set(users);
      },
      error: (err: HttpErrorResponse) => {
        this.loading.set(false);
        this.error.set(err.error?.error ?? 'Erreur lors du chargement des comptes');
      },
    });
  }

  save(user: AdminUser): void {
    this.savingId.set(user.id);
    this.savedId.set(null);
    this.error.set(null);
    this.adminService.updatePermissions(user.id, user.root, user.permissions).subscribe({
      next: () => {
        this.savingId.set(null);
        this.savedId.set(user.id);
      },
      error: (err: HttpErrorResponse) => {
        this.savingId.set(null);
        this.error.set(err.error?.error ?? "Erreur lors de l'enregistrement");
      },
    });
  }

  delete(user: AdminUser): void {
    if (!confirm(`Supprimer le compte ${user.email} ? Cette action est irréversible.`)) return;

    this.deletingId.set(user.id);
    this.error.set(null);
    this.adminService.deleteUser(user.id).subscribe({
      next: () => {
        this.deletingId.set(null);
        this.users.update((users) => users.filter((u) => u.id !== user.id));
      },
      error: (err: HttpErrorResponse) => {
        this.deletingId.set(null);
        this.error.set(err.error?.error ?? 'Erreur lors de la suppression');
      },
    });
  }

  // Whether every permission (Root excluded — it already bypasses them all)
  // is currently checked for user, for the row's "check all" checkbox to
  // reflect.
  allChecked(user: AdminUser): boolean {
    return this.permissionKeys.every((key) => user.permissions[key]);
  }

  toggleAll(user: AdminUser, event: Event): void {
    const checked = (event.target as HTMLInputElement).checked;
    for (const key of this.permissionKeys) {
      user.permissions[key] = checked;
    }
  }

  // Whether every editable (non-root) user currently has this permission
  // checked, for the column header checkbox to reflect. Root users are
  // excluded since their checkboxes are disabled and meaningless here.
  columnChecked(key: keyof AdminUser['permissions']): boolean {
    const editable = this.users().filter((u) => !u.root);
    return editable.length > 0 && editable.every((u) => u.permissions[key]);
  }

  toggleColumn(key: keyof AdminUser['permissions'], event: Event): void {
    const checked = (event.target as HTMLInputElement).checked;
    for (const user of this.users()) {
      if (user.root) continue;
      user.permissions[key] = checked;
    }
  }
}
