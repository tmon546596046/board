import { Component, Input, Output, EventEmitter } from "@angular/core";
import { User } from '../user';
import { UserService } from "../user-service/user-service"
import { MessageService } from "../../shared/message-service/message.service";

export enum editModel { emNew, emEdit }

@Component({
  selector: "new-user",
  templateUrl: "./user-new-edit.component.html",
  styleUrls: ["./user-new-edit.component.css"]
})
export class NewEditUserComponent {
  _isOpen: boolean;
  isAlertOpen: boolean = false;
  afterCommitErr: string = "";

  constructor(private userService: UserService,
              private messageService: MessageService) {
  };

  @Input() userModel: User;
  @Input() CurEditModel: editModel;

  @Input()
  get isOpen() {
    return this._isOpen;
  }

  set isOpen(open: boolean) {
    this._isOpen = open;
    this.isOpenChange.emit(this._isOpen);
  }

  @Output() isOpenChange: EventEmitter<boolean> = new EventEmitter<boolean>();
  @Output() SubmitSuccessEvent: EventEmitter<any> = new EventEmitter<any>();

  get Title() {
    return this.CurEditModel == editModel.emNew
      ? "USER_CENTER.ADD_USER"
      : "USER_CENTER.EDIT_USER";
  }

  get ActionCaption() {
    return this.CurEditModel == editModel.emNew
      ? "USER_CENTER.ADD"
      : "USER_CENTER.EDIT";
  }

  submitUser() {
    this.CurEditModel == editModel.emEdit ? this.updateUser() : this.addNewUser();
  }

  updateUser() {
    this.userService.updateUser(this.userModel)
      .then(() => {
        this.SubmitSuccessEvent.emit(true);
        this.isOpen = false;
      })
      .catch(err => {
        if(err) {
          if(err.status === 400) {
            this.isAlertOpen = true;
            this.afterCommitErr = 'ACCOUNT.EMAIL_IS_ILLEGAL';
          } else if(err.status === 409){
            this.isAlertOpen = true;
            this.afterCommitErr = 'ACCOUNT.EMAIL_ALREADY_EXISTS';
          } else {
            this.isOpen = false;
            this.messageService.dispatchError(err)
          }
        }
      });
  }

  addNewUser() {
    this.userService.newUser(this.userModel)
      .then(() => {
        this.SubmitSuccessEvent.emit(true);
        this.isOpen = false;
      })
      .catch(err => {
        if(err) {
          if(err.status === 400) {
            this.isAlertOpen = true;
            this.afterCommitErr = 'ACCOUNT.EMAIL_IS_ILLEGAL';
          } else if(err.status === 409){
            this.isAlertOpen = true;
            this.afterCommitErr = 'ACCOUNT.EMAIL_ALREADY_EXISTS';
          } else {
            this.isOpen = false;
            this.messageService.dispatchError(err)
          }
        }
      });
  }

}