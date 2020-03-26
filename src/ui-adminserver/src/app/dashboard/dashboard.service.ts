import { Injectable } from '@angular/core';
import { HttpClient, HttpResponse } from '@angular/common/http';
import { Observable } from 'rxjs';
import { map } from 'rxjs/operators';
import { ComponentStatus } from './component-status.model';
import { User } from '../account/account.model';

const BASE_URL = '/v1/admin';

@Injectable()
export class DashboardService {

  constructor(private http: HttpClient,
              private token: string) {
    this.token = window.sessionStorage.getItem('token');
  }

  restartBoard(user: User): Observable<any> {
    return this.http.post(
      `${BASE_URL}/board/restart?token=${this.token}`,
      user.PostBody()
    );
  }

  shutdownBoard(user: User): Observable<any> {
    return this.http.get(
      `${BASE_URL}/board/shutdown?token=${this.token}`,
      user.PostBody()
    );
  }

  monitorContainer(): Observable<any> {
    return this.http.get(
      `${BASE_URL}/monitor?token=${this.token}`,
      { observe: 'response', })
      .pipe(map((res: HttpResponse<Array<ComponentStatus>>) => {
        return res.body;
      }));
  }
}
