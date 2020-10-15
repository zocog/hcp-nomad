import Component from '@glimmer/component';
import { action } from '@ember/object';

export default class DasErrorComponent extends Component {
  @action
  proceedManually() {
    const manualPromise = new Promise(resolve => {
      this.manualPromiseResolve = resolve;
    });

    this.args.proceed.perform(manualPromise);
  }

  @action
  dismissClicked() {
    this.manualPromiseResolve(true);
  }
}
