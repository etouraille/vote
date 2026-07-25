import { inject } from '@angular/core';
import { CanActivateFn, Router } from '@angular/router';
import { catchError, map, of } from 'rxjs';
import { AuthService } from '../service/auth';

// Guards the backoffice route: must be authenticated, and the caller's JWT
// must carry Root — checked server-side too (every /api/admin/... route
// re-verifies it), this is purely to keep non-admins out of the UI.
export const adminGuard: CanActivateFn = () => {
  const auth = inject(AuthService);
  const router = inject(Router);

  if (!auth.isAuthenticated()) {
    return router.createUrlTree(['/']);
  }

  return auth.me().pipe(
    map((me) => (me.root ? true : router.createUrlTree(['/home']))),
    catchError(() => of(router.createUrlTree(['/home']))),
  );
};
